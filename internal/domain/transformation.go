package domain

type TransformationType string

const (
	TransformGrid             TransformationType = "grid"
	TransformRemoveCoordinate TransformationType = "remove_coordinate"
	TransformReplaceName      TransformationType = "replace_name"
	TransformClipLayer        TransformationType = "clip_layer"
)

type Transformation struct {
	Type            TransformationType `json:"type"`
	SourceSiteID    string             `json:"sourceSiteId,omitempty"`
	LayerName       string             `json:"layerName,omitempty"`
	GridMeters      float64            `json:"gridMeters,omitempty"`
	ReplacementName string             `json:"replacementName,omitempty"`
}

type TransformationImpact struct {
	TransformationIndex int      `json:"transformationIndex"`
	MatchedFeatureCount int      `json:"matchedFeatureCount"`
	AffectedLayers      []string `json:"affectedLayers"`
}
