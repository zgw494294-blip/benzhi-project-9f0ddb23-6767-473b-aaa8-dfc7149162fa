package domain

type CoverageStatus string

const (
	CoverageCovered   CoverageStatus = "covered"
	CoverageUncovered CoverageStatus = "uncovered"
)

type CoverageItem struct {
	SensitiveSiteID           string               `json:"sensitiveSiteId"`
	Status                    CoverageStatus       `json:"status"`
	TransformationTypes       []TransformationType `json:"transformationTypes"`
	AffectedLayers            []string             `json:"affectedLayers"`
	AffectedFeatureCount      int                  `json:"affectedFeatureCount"`
	PublicPrecisionConclusion string               `json:"publicPrecisionConclusion"`
}

type PreviewWarning struct {
	Code     string `json:"code"`
	Location string `json:"location"`
	Message  string `json:"message"`
}

type TransformationPreview struct {
	PublicLayers          []PublicLayer          `json:"-"`
	TransformationImpacts []TransformationImpact `json:"-"`
	Coverage              []CoverageItem         `json:"coverage"`
	Warnings              []PreviewWarning       `json:"warnings"`
	ContentDigest         string                 `json:"contentDigest"`
}
