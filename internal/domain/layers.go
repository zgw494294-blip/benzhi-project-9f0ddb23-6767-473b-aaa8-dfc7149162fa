package domain

type LayerSummary struct {
	Name         string `json:"name"`
	FeatureCount int    `json:"featureCount"`
	Description  string `json:"description,omitempty"`
}

type PublicFeature struct {
	ID           string      `json:"id"`
	Name         string      `json:"name,omitempty"`
	SourceSiteID string      `json:"sourceSiteId,omitempty"`
	Coordinate   *Coordinate `json:"coordinate,omitempty"`
}

type PublicLayer struct {
	Name     string          `json:"name"`
	Features []PublicFeature `json:"features"`
}
