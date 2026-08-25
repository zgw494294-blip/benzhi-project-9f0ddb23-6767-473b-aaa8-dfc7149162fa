package domain

import (
	"fmt"
	"math"
	"sort"
)

// RunChecks 按固定规则顺序检查已经执行变换后的候选公开版。
func RunChecks(p *SurveyPackage, revision *RedactionRevision, idFactory func() string) []ReviewFinding {
	findings := make([]ReviewFinding, 0)
	sites := make(map[string]SensitiveSite, len(p.SensitiveSites))
	for _, site := range p.SensitiveSites {
		sites[site.ID] = site
	}
	registered := make(map[string]bool, len(p.LayerSummaries))
	registeredCounts := make(map[string]int, len(p.LayerSummaries))
	for _, layer := range p.LayerSummaries {
		registered[layer.Name], registeredCounts[layer.Name] = true, layer.FeatureCount
	}
	clipped := map[string]bool{}
	for _, tr := range revision.Transformations {
		if tr.Type == TransformClipLayer {
			clipped[tr.LayerName] = true
		}
	}
	layerNames, publicNames, featureIDs := map[string]bool{}, map[string]bool{}, map[string]string{}
	for li, layer := range revision.PublicLayers {
		layerLocation := fmt.Sprintf("publicLayers[%d]", li)
		if layer.Name == "" {
			findings = append(findings, checkedFinding(revision.ID, "PUBLIC_LAYER_NAME_EMPTY", "error", layerLocation+".name", "公开图层名称不能为空"))
		}
		if layerNames[layer.Name] {
			findings = append(findings, checkedFinding(revision.ID, "PUBLIC_LAYER_NAME_DUPLICATE", "error", layerLocation+".name", "公开图层名称重复"))
		}
		layerNames[layer.Name], publicNames[layer.Name] = true, true
		if !registered[layer.Name] {
			findings = append(findings, checkedFinding(revision.ID, "REFERENCE_UNKNOWN_LAYER", "error", layerLocation+".name", "公开图层未在成果包摘要中登记"))
		}
		if registered[layer.Name] && !clipped[layer.Name] && len(layer.Features) != registeredCounts[layer.Name] {
			findings = append(findings, checkedFinding(revision.ID, "PUBLIC_FEATURE_COUNT_DRIFT", "warning", layerLocation+".features", "公开要素数量与登记图层摘要不一致"))
		}
		for fi, feature := range layer.Features {
			location := fmt.Sprintf("publicLayers[%d].features[%d]", li, fi)
			if feature.ID == "" {
				findings = append(findings, checkedFinding(revision.ID, "PUBLIC_FEATURE_ID_EMPTY", "error", location+".id", "公开要素 id 不能为空"))
			}
			if first, exists := featureIDs[feature.ID]; exists {
				findings = append(findings, checkedFinding(revision.ID, "PUBLIC_FEATURE_ID_DUPLICATE", "error", location+".id", "公开要素 id 重复，首次出现于 "+first))
			} else {
				featureIDs[feature.ID] = location
			}
			if feature.SourceSiteID != "" {
				if _, ok := sites[feature.SourceSiteID]; !ok {
					findings = append(findings, checkedFinding(revision.ID, "REFERENCE_UNKNOWN_SITE", "error", location+".sourceSiteId", "要素引用了未登记的敏感点位"))
				}
			}
			if feature.Coordinate == nil {
				continue
			}
			if !p.SurveyBounds.Contains(*feature.Coordinate) {
				findings = append(findings, checkedFinding(revision.ID, "PUBLIC_COORDINATE_OUT_OF_BOUNDS", "error", location+".coordinate", "公开坐标超出成果包测绘范围"))
			}
			for _, site := range p.SensitiveSites {
				distance := math.Hypot(feature.Coordinate.X-site.OriginalCoordinate.X, feature.Coordinate.Y-site.OriginalCoordinate.Y)
				if distance < site.RecommendedPrecisionMeters {
					findings = append(findings, checkedFinding(revision.ID, "COORDINATE_LEAK", "critical", location+".coordinate", "公开坐标精度高于敏感点位 "+site.ID+" 的允许精度"))
				}
			}
		}
	}
	for i, layer := range p.LayerSummaries {
		if !publicNames[layer.Name] && !clipped[layer.Name] {
			findings = append(findings, checkedFinding(revision.ID, "REGISTERED_LAYER_MISSING", "warning", fmt.Sprintf("layerSummaries[%d].name", i), "登记图层未出现在候选公开版中"))
		}
	}
	for ti, tr := range revision.Transformations {
		location := fmt.Sprintf("transformations[%d]", ti)
		for previous := 0; previous < ti; previous++ {
			if transformationsOverlapFeature(revision.PublicLayers, revision.Transformations[previous], tr) && transformationsConflict(revision.Transformations[previous].Type, tr.Type) {
				findings = append(findings, checkedFinding(revision.ID, "TRANSFORMATION_CONFLICT", "error", location, fmt.Sprintf("与 transformations[%d] 在同一来源要素上冲突", previous)))
				break
			}
		}
		if tr.SourceSiteID != "" {
			if _, ok := sites[tr.SourceSiteID]; !ok {
				findings = append(findings, checkedFinding(revision.ID, "TRANSFORM_SOURCE_MISSING", "error", location+".sourceSiteId", "脱敏变换来源点位不存在"))
				continue
			}
		}
		hits := transformedFeatureHits(revision.PublicLayers, tr)
		if ti < len(revision.TransformationImpacts) {
			hits = revision.TransformationImpacts[ti].MatchedFeatureCount
		}
		if tr.Type != TransformClipLayer && hits == 0 {
			findings = append(findings, checkedFinding(revision.ID, "TRANSFORMATION_NO_EFFECT", "error", location, "声明的脱敏变换未命中来源要素"))
			continue
		}
		site := sites[tr.SourceSiteID]
		switch tr.Type {
		case TransformGrid:
			if tr.GridMeters < site.RecommendedPrecisionMeters {
				findings = append(findings, checkedFinding(revision.ID, "GRID_PRECISION_INSUFFICIENT", "error", location+".gridMeters", "网格化结果未达到建议公开精度"))
			}
		case TransformRemoveCoordinate:
			if transformResultViolates(revision.PublicLayers, tr, func(f PublicFeature) bool { return f.Coordinate != nil }) {
				findings = append(findings, checkedFinding(revision.ID, "COORDINATE_REMOVAL_INEFFECTIVE", "critical", location, "坐标移除策略未产生预期结果"))
			}
		case TransformReplaceName:
			if transformResultViolates(revision.PublicLayers, tr, func(f PublicFeature) bool { return f.Name != tr.ReplacementName }) {
				findings = append(findings, checkedFinding(revision.ID, "NAME_REPLACEMENT_INEFFECTIVE", "error", location, "名称替换策略未产生预期结果"))
			}
		case TransformClipLayer:
			for _, layer := range revision.PublicLayers {
				if layer.Name == tr.LayerName {
					findings = append(findings, checkedFinding(revision.ID, "LAYER_CLIP_INEFFECTIVE", "error", location, "图层裁剪策略未产生预期结果"))
					break
				}
			}
		}
	}
	findings = deduplicateAndSort(findings)
	// 兼容旧调用方的可注入标识；生产链路传 nil，使用稳定指纹标识。
	if idFactory != nil {
		for i := range findings {
			findings[i].ID = idFactory()
		}
	}
	return findings
}

func transformedFeatureHits(layers []PublicLayer, tr Transformation) int {
	hits := 0
	for _, layer := range layers {
		if tr.LayerName == "" || layer.Name == tr.LayerName {
			for _, f := range layer.Features {
				if f.SourceSiteID == tr.SourceSiteID {
					hits++
				}
			}
		}
	}
	return hits
}

func transformResultViolates(layers []PublicLayer, tr Transformation, invalid func(PublicFeature) bool) bool {
	for _, layer := range layers {
		if tr.LayerName == "" || layer.Name == tr.LayerName {
			for _, feature := range layer.Features {
				if feature.SourceSiteID == tr.SourceSiteID && invalid(feature) {
					return true
				}
			}
		}
	}
	return false
}

func checkedFinding(revisionID, code, severity, location, message string) ReviewFinding {
	fingerprint := StableDigest(struct{ RevisionID, RuleCode, Location, Message string }{revisionID, code, location, message})
	return ReviewFinding{ID: "finding_" + fingerprint[:24], RuleCode: code, Severity: severity, Location: location, Message: message, Status: FindingOpen}
}

func deduplicateAndSort(findings []ReviewFinding) []ReviewFinding {
	seen := map[string]bool{}
	out := make([]ReviewFinding, 0, len(findings))
	for _, finding := range findings {
		if !seen[finding.ID] {
			seen[finding.ID] = true
			out = append(out, finding)
		}
	}
	rank := map[string]int{"critical": 0, "error": 1, "warning": 2}
	sort.Slice(out, func(i, j int) bool {
		if rank[out[i].Severity] != rank[out[j].Severity] {
			return rank[out[i].Severity] < rank[out[j].Severity]
		}
		if out[i].RuleCode != out[j].RuleCode {
			return out[i].RuleCode < out[j].RuleCode
		}
		if out[i].Location != out[j].Location {
			return out[i].Location < out[j].Location
		}
		return out[i].ID < out[j].ID
	})
	return out
}
