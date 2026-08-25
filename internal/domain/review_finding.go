package domain

import "time"

type FindingStatus string

const (
	FindingOpen     FindingStatus = "open"
	FindingResolved FindingStatus = "resolved"
	FindingAccepted FindingStatus = "accepted"
)

type ReviewFinding struct {
	ID              string                  `json:"id"`
	PackageID       string                  `json:"packageId"`
	RevisionID      string                  `json:"revisionId"`
	RuleCode        string                  `json:"ruleCode"`
	Severity        string                  `json:"severity"`
	Location        string                  `json:"location"`
	Message         string                  `json:"message"`
	Status          FindingStatus           `json:"status"`
	Resolution      string                  `json:"resolution,omitempty"`
	DecidedBy       string                  `json:"decidedBy,omitempty"`
	DecidedAt       *time.Time              `json:"decidedAt,omitempty"`
	DecisionVersion int                     `json:"decisionVersion,omitempty"`
	DecisionHistory []FindingDecisionRecord `json:"decisionHistory,omitempty"`
}

type FindingDecisionRecord struct {
	Version          int           `json:"version"`
	OldStatus        FindingStatus `json:"oldStatus"`
	NewStatus        FindingStatus `json:"newStatus"`
	Resolution       string        `json:"resolution"`
	CorrectionReason string        `json:"correctionReason,omitempty"`
	Reviewer         string        `json:"reviewer"`
	DecidedAt        time.Time     `json:"decidedAt"`
}
