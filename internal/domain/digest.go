package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

func StableDigest(value any) string {
	b, _ := json.Marshal(value)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func PublicLocationExists(layers []PublicLayer, location string) bool {
	const prefix = "publicLayers["
	if !strings.HasPrefix(location, prefix) {
		return false
	}
	rest := strings.TrimPrefix(location, prefix)
	parts := strings.Split(rest, "].features[")
	if len(parts) != 2 || !strings.HasSuffix(parts[1], "]") {
		return false
	}
	li, err1 := strconv.Atoi(parts[0])
	fi, err2 := strconv.Atoi(strings.TrimSuffix(parts[1], "]"))
	return err1 == nil && err2 == nil && li >= 0 && li < len(layers) && fi >= 0 && fi < len(layers[li].Features)
}

func BaseDigest(p *SurveyPackage) string {
	type siteItem struct {
		ID               string
		Revision         int
		Category         string
		Coordinate       Coordinate
		ProtectionReason string
		Precision        float64
	}
	type base struct {
		CaveName       string
		Bounds         Bounds
		CRS            string
		Layers         []LayerSummary
		SensitiveSites []siteItem
	}
	sites := make([]siteItem, 0, len(p.SensitiveSites))
	for _, site := range p.SensitiveSites {
		sites = append(sites, siteItem{site.ID, site.Revision, site.Category, site.OriginalCoordinate, site.ProtectionReason, site.RecommendedPrecisionMeters})
	}
	sort.Slice(sites, func(i, j int) bool { return sites[i].ID < sites[j].ID })
	return StableDigest(base{p.CaveName, p.SurveyBounds, p.CoordinateReferenceSystem, p.LayerSummaries, sites})
}

func ApplyTransformations(layers []PublicLayer, sites []SensitiveSite, transforms []Transformation) ([]PublicLayer, error) {
	out, _, err := executeTransformations(layers, sites, transforms)
	return out, err
}

func executeTransformations(layers []PublicLayer, sites []SensitiveSite, transforms []Transformation) ([]PublicLayer, []TransformationImpact, error) {
	out := cloneLayers(layers)
	impacts := make([]TransformationImpact, 0, len(transforms))
	siteByID := make(map[string]SensitiveSite, len(sites))
	for _, site := range sites {
		siteByID[site.ID] = site
	}
	for i, tr := range transforms {
		if err := validateTransformation(tr, siteByID); err != nil {
			return nil, nil, fmt.Errorf("transformation[%d]: %w", i, err)
		}
		impact := TransformationImpact{TransformationIndex: i}
		layerSet := map[string]bool{}
		switch tr.Type {
		case TransformClipLayer:
			for _, layer := range out {
				if layer.Name == tr.LayerName {
					impact.MatchedFeatureCount += len(layer.Features)
					layerSet[layer.Name] = true
				}
			}
			out = clipLayer(out, tr.LayerName)
		default:
			for li := range out {
				if tr.LayerName != "" && out[li].Name != tr.LayerName {
					continue
				}
				for fi := range out[li].Features {
					f := &out[li].Features[fi]
					if f.SourceSiteID != tr.SourceSiteID {
						continue
					}
					impact.MatchedFeatureCount++
					layerSet[out[li].Name] = true
					switch tr.Type {
					case TransformRemoveCoordinate:
						f.Coordinate = nil
					case TransformReplaceName:
						f.Name = tr.ReplacementName
					case TransformGrid:
						if f.Coordinate != nil {
							f.Coordinate.X = roundGrid(f.Coordinate.X, tr.GridMeters)
							f.Coordinate.Y = roundGrid(f.Coordinate.Y, tr.GridMeters)
						}
					}
				}
			}
		}
		for name := range layerSet {
			impact.AffectedLayers = append(impact.AffectedLayers, name)
		}
		sort.Strings(impact.AffectedLayers)
		impacts = append(impacts, impact)
	}
	return out, impacts, nil
}

func PreviewTransformations(layers []PublicLayer, sites []SensitiveSite, transforms []Transformation) (TransformationPreview, error) {
	result, impacts, err := executeTransformations(layers, sites, transforms)
	if err != nil {
		return TransformationPreview{}, err
	}
	preview := TransformationPreview{PublicLayers: result, TransformationImpacts: impacts, ContentDigest: StableDigest(result)}
	warnings := make([]PreviewWarning, 0)
	clipSeen := map[string]int{}
	for i, tr := range transforms {
		if tr.Type == TransformClipLayer {
			if previous, ok := clipSeen[tr.LayerName]; ok {
				warnings = append(warnings, PreviewWarning{Code: "DUPLICATE_CLIP", Location: fmt.Sprintf("transformations[%d]", i), Message: fmt.Sprintf("与 transformations[%d] 重复裁剪同一图层", previous)})
			}
			clipSeen[tr.LayerName] = i
			continue
		}
		for previous := 0; previous < i; previous++ {
			if transformationsOverlapFeature(layers, transforms[previous], tr) && transformationsConflict(transforms[previous].Type, tr.Type) {
				warnings = append(warnings, PreviewWarning{Code: "TRANSFORMATION_CONFLICT", Location: fmt.Sprintf("transformations[%d]", i), Message: fmt.Sprintf("与 transformations[%d] 在同一来源要素上冲突", previous)})
				break
			}
		}
	}
	for _, site := range sites {
		item := CoverageItem{SensitiveSiteID: site.ID, Status: CoverageUncovered, TransformationTypes: []TransformationType{}, AffectedLayers: []string{}, PublicPrecisionConclusion: "未覆盖"}
		layerSet := map[string]bool{}
		for i, tr := range transforms {
			matchesSite := tr.SourceSiteID == site.ID
			if tr.Type == TransformClipLayer {
				for _, layer := range layers {
					if layer.Name == tr.LayerName && layerHasSource(layer, site.ID) {
						matchesSite = true
						break
					}
				}
			}
			if !matchesSite {
				continue
			}
			item.TransformationTypes = append(item.TransformationTypes, tr.Type)
			hits := impacts[i].MatchedFeatureCount
			if tr.Type == TransformClipLayer {
				hits = 0
				for _, layer := range layers {
					if layer.Name == tr.LayerName {
						for _, feature := range layer.Features {
							if feature.SourceSiteID == site.ID {
								hits++
							}
						}
					}
				}
			}
			item.AffectedFeatureCount += hits
			for _, layer := range layers {
				if (tr.LayerName == "" || tr.LayerName == layer.Name) && layerHasSource(layer, site.ID) {
					layerSet[layer.Name] = true
				}
			}
			if hits == 0 {
				warnings = append(warnings, PreviewWarning{Code: "TRANSFORMATION_NO_MATCH", Location: fmt.Sprintf("transformations[%d]", i), Message: "来源点位变换未命中任何公开要素"})
			}
			if tr.Type == TransformGrid && tr.GridMeters < site.RecommendedPrecisionMeters {
				warnings = append(warnings, PreviewWarning{Code: "GRID_PRECISION_TOO_FINE", Location: fmt.Sprintf("transformations[%d].gridMeters", i), Message: "网格精度低于点位建议公开精度"})
			}
		}
		for name := range layerSet {
			item.AffectedLayers = append(item.AffectedLayers, name)
		}
		sort.Strings(item.AffectedLayers)
		if item.AffectedFeatureCount > 0 {
			item.Status = CoverageCovered
			item.PublicPrecisionConclusion = precisionConclusion(site, transforms)
		} else {
			warnings = append(warnings, PreviewWarning{Code: "SENSITIVE_SITE_UNCOVERED", Location: "sensitiveSites[" + site.ID + "]", Message: "敏感点位未被有效变换覆盖"})
		}
		preview.Coverage = append(preview.Coverage, item)
	}
	preview.Warnings = warnings
	return preview, nil
}

func transformationsConflict(a, b TransformationType) bool {
	return (a == TransformRemoveCoordinate && b == TransformGrid) || (a == TransformGrid && b == TransformRemoveCoordinate) || (a == TransformReplaceName && b == TransformReplaceName)
}

func transformationsOverlapFeature(layers []PublicLayer, a, b Transformation) bool {
	if a.Type == TransformClipLayer || b.Type == TransformClipLayer || a.SourceSiteID == "" || a.SourceSiteID != b.SourceSiteID {
		return false
	}
	if a.LayerName != "" && b.LayerName != "" && a.LayerName != b.LayerName {
		return false
	}
	for _, layer := range layers {
		if a.LayerName != "" && layer.Name != a.LayerName {
			continue
		}
		if b.LayerName != "" && layer.Name != b.LayerName {
			continue
		}
		if layerHasSource(layer, a.SourceSiteID) {
			return true
		}
	}
	return false
}

func transformationHits(layers []PublicLayer, tr Transformation) int {
	if tr.Type == TransformClipLayer {
		for _, layer := range layers {
			if layer.Name == tr.LayerName {
				return len(layer.Features)
			}
		}
		return 0
	}
	hits := 0
	for _, layer := range layers {
		if tr.LayerName != "" && layer.Name != tr.LayerName {
			continue
		}
		for _, feature := range layer.Features {
			if feature.SourceSiteID == tr.SourceSiteID {
				hits++
			}
		}
	}
	return hits
}

func layerHasSource(layer PublicLayer, siteID string) bool {
	for _, feature := range layer.Features {
		if feature.SourceSiteID == siteID {
			return true
		}
	}
	return false
}

func precisionConclusion(site SensitiveSite, transforms []Transformation) string {
	conclusion := "已变换但公开精度不足"
	for _, tr := range transforms {
		if tr.SourceSiteID != site.ID {
			continue
		}
		if tr.Type == TransformRemoveCoordinate {
			return "坐标已移除"
		}
		if tr.Type == TransformGrid && tr.GridMeters >= site.RecommendedPrecisionMeters {
			conclusion = "达到建议公开精度"
		}
		if tr.Type == TransformClipLayer {
			conclusion = "来源图层已裁剪"
		}
	}
	return conclusion
}

func validateTransformation(tr Transformation, sites map[string]SensitiveSite) error {
	switch tr.Type {
	case TransformGrid, TransformRemoveCoordinate, TransformReplaceName:
		if _, ok := sites[tr.SourceSiteID]; !ok {
			return Invalid("sourceSiteId", "必须引用已登记敏感点位")
		}
	case TransformClipLayer:
		if strings.TrimSpace(tr.LayerName) == "" {
			return Invalid("layerName", "图层裁剪必须指定图层")
		}
	default:
		return Invalid("type", "不支持的脱敏变换")
	}
	if tr.Type == TransformGrid && tr.GridMeters <= 0 {
		return Invalid("gridMeters", "必须大于零")
	}
	if tr.Type == TransformReplaceName && strings.TrimSpace(tr.ReplacementName) == "" {
		return Invalid("replacementName", "名称替换不能为空")
	}
	return nil
}

func cloneLayers(in []PublicLayer) []PublicLayer {
	out := make([]PublicLayer, len(in))
	for i := range in {
		out[i].Name = in[i].Name
		out[i].Features = make([]PublicFeature, len(in[i].Features))
		copy(out[i].Features, in[i].Features)
		for j := range out[i].Features {
			if in[i].Features[j].Coordinate != nil {
				c := *in[i].Features[j].Coordinate
				out[i].Features[j].Coordinate = &c
			}
		}
	}
	return out
}

func clipLayer(layers []PublicLayer, name string) []PublicLayer {
	out := make([]PublicLayer, 0, len(layers))
	for _, layer := range layers {
		if layer.Name != name {
			out = append(out, layer)
		}
	}
	return out
}

func roundGrid(value, grid float64) float64 {
	return math.Round(value/grid) * grid
}
