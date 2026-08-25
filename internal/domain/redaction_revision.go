package domain

import "time"

type RedactionRevision struct {
	ID                    string                 `json:"id"`
	PackageID             string                 `json:"packageId"`
	Sequence              int                    `json:"sequence"`
	BaseDigest            string                 `json:"baseDigest"`
	Transformations       []Transformation       `json:"transformations"`
	TransformationImpacts []TransformationImpact `json:"transformationImpacts,omitempty"`
	PublicLayers          []PublicLayer          `json:"publicLayers"`
	ContentDigest         string                 `json:"contentDigest"`
	SubmittedBy           string                 `json:"submittedBy"`
	SubmittedAt           time.Time              `json:"submittedAt"`
	SupersedesRevisionID  string                 `json:"supersedesRevisionId,omitempty"`
	RemediationMappings   []RemediationMapping   `json:"remediationMappings,omitempty"`
	RemediationResults    []RemediationResult    `json:"remediationResults,omitempty"`
}

type RemediationMapping struct {
	FindingID           string `json:"findingId"`
	Explanation         string `json:"explanation"`
	TransformationIndex *int   `json:"transformationIndex,omitempty"`
	PublicLocation      string `json:"publicLocation,omitempty"`
}

type RemediationOutcome string

const (
	RemediationEliminated  RemediationOutcome = "eliminated"
	RemediationReproduced  RemediationOutcome = "reproduced"
	RemediationUnlocatable RemediationOutcome = "unlocatable"
)

type RemediationResult struct {
	FindingID    string             `json:"findingId"`
	Outcome      RemediationOutcome `json:"outcome"`
	Explanation  string             `json:"explanation"`
	NewFindingID string             `json:"newFindingId,omitempty"`
}
