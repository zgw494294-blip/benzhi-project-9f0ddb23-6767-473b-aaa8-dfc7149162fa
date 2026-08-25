package domain

import "time"

type SensitiveSite struct {
	ID                         string     `json:"id"`
	PackageID                  string     `json:"packageId"`
	Category                   string     `json:"category"`
	OriginalCoordinate         Coordinate `json:"originalCoordinate"`
	ProtectionReason           string     `json:"protectionReason"`
	RecommendedPrecisionMeters float64    `json:"recommendedPrecisionMeters"`
	Revision                   int        `json:"revision"`
	RecordedBy                 string     `json:"recordedBy"`
	RecordedAt                 time.Time  `json:"recordedAt"`
	SupersedesRevision         int        `json:"supersedesRevision,omitempty"`
	CorrectionReason           string     `json:"correctionReason,omitempty"`
}
