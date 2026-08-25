package domain

import "time"

type ReleaseCredential struct {
	ID               string    `json:"id"`
	PackageID        string    `json:"packageId"`
	RevisionID       string    `json:"revisionId"`
	ContentDigest    string    `json:"contentDigest"`
	PolicyDigest     string    `json:"policyDigest"`
	ManifestDigest   string    `json:"manifestDigest"`
	IssuedBy         string    `json:"issuedBy"`
	IssuedAt         time.Time `json:"issuedAt"`
	VerificationHash string    `json:"verificationHash"`
}

type ManifestLayer struct {
	Name         string `json:"name"`
	FeatureCount int    `json:"featureCount"`
	LayerDigest  string `json:"layerDigest"`
}

type TransformationCount struct {
	Type  TransformationType `json:"type"`
	Count int                `json:"count"`
}

type DecisionSummary struct {
	Open     int `json:"open"`
	Resolved int `json:"resolved"`
	Accepted int `json:"accepted"`
}

type ReleaseManifest struct {
	PackageID             string                `json:"packageId"`
	RevisionID            string                `json:"revisionId"`
	Layers                []ManifestLayer       `json:"layers"`
	TransformationSummary []TransformationCount `json:"transformationSummary"`
	DecisionSummary       DecisionSummary       `json:"decisionSummary"`
	IssuedAt              time.Time             `json:"issuedAt"`
	ManifestDigest        string                `json:"manifestDigest"`
}
