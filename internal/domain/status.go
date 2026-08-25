package domain

type PackageStatus string

const (
	StatusDraft         PackageStatus = "draft"
	StatusPendingCheck  PackageStatus = "pending_check"
	StatusPendingReview PackageStatus = "pending_review"
	StatusReturned      PackageStatus = "returned"
	StatusApproved      PackageStatus = "approved"
	StatusFrozen        PackageStatus = "frozen"
	StatusPublished     PackageStatus = "published"
)
