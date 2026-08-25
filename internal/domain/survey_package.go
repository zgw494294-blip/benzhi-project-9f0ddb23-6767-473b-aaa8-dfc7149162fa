package domain

import "time"

type SurveyPackage struct {
	ID                        string                     `json:"id"`
	CaveName                  string                     `json:"caveName"`
	SurveyBounds              Bounds                     `json:"surveyBounds"`
	CoordinateReferenceSystem string                     `json:"coordinateReferenceSystem"`
	LayerSummaries            []LayerSummary             `json:"layerSummaries"`
	Owner                     string                     `json:"owner"`
	Status                    PackageStatus              `json:"status"`
	Version                   int64                      `json:"version"`
	CreatedAt                 time.Time                  `json:"createdAt"`
	UpdatedAt                 time.Time                  `json:"updatedAt"`
	SensitiveSites            []SensitiveSite            `json:"sensitiveSites"`
	SensitiveSiteHistory      map[string][]SensitiveSite `json:"sensitiveSiteHistory,omitempty"`
	RedactionRevisions        []RedactionRevision        `json:"redactionRevisions"`
	Findings                  []ReviewFinding            `json:"findings"`
	Credential                *ReleaseCredential         `json:"credential,omitempty"`
	ReleaseManifest           *ReleaseManifest           `json:"releaseManifest,omitempty"`
	Audit                     []AuditEvent               `json:"audit"`
}
