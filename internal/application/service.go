package application

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"karst-map-release/internal/domain"
	"karst-map-release/internal/repository"
)

type Service struct {
	store repository.Store
	now   func() time.Time
	ids   func(string) string
	locks *keyedLocks
}

func NewService(store repository.Store) *Service {
	return &Service{store: store, now: time.Now, ids: randomID, locks: newKeyedLocks()}
}

type CreatePackageCommand struct {
	CaveName                  string                `json:"caveName"`
	SurveyBounds              domain.Bounds         `json:"surveyBounds"`
	CoordinateReferenceSystem string                `json:"coordinateReferenceSystem"`
	LayerSummaries            []domain.LayerSummary `json:"layerSummaries"`
	Owner                     string                `json:"owner"`
	ExpectedVersion           int64                 `json:"expectedVersion"`
}

type AddSensitiveSiteCommand struct {
	Category                   string            `json:"category"`
	OriginalCoordinate         domain.Coordinate `json:"originalCoordinate"`
	ProtectionReason           string            `json:"protectionReason"`
	RecommendedPrecisionMeters float64           `json:"recommendedPrecisionMeters"`
	RecordedBy                 string            `json:"recordedBy"`
	ExpectedVersion            int64             `json:"expectedVersion"`
}

type ReviseMetadataCommand struct {
	CaveName                  string                `json:"caveName"`
	SurveyBounds              domain.Bounds         `json:"surveyBounds"`
	CoordinateReferenceSystem string                `json:"coordinateReferenceSystem"`
	LayerSummaries            []domain.LayerSummary `json:"layerSummaries"`
	Owner                     string                `json:"owner"`
	Actor                     string                `json:"actor,omitempty"`
	RevisedBy                 string                `json:"revisedBy,omitempty"`
	RevisionReason            string                `json:"revisionReason"`
	ExpectedVersion           int64                 `json:"expectedVersion"`
}

type ReviseSensitiveSiteCommand struct {
	Category                   string            `json:"category"`
	OriginalCoordinate         domain.Coordinate `json:"originalCoordinate"`
	ProtectionReason           string            `json:"protectionReason"`
	RecommendedPrecisionMeters float64           `json:"recommendedPrecisionMeters"`
	RecordedBy                 string            `json:"recordedBy"`
	SupersedesRevision         int               `json:"supersedesRevision"`
	CorrectionReason           string            `json:"correctionReason"`
	ExpectedVersion            int64             `json:"expectedVersion"`
}

type SubmitRevisionCommand struct {
	BaseDigest           string                      `json:"baseDigest"`
	Transformations      []domain.Transformation     `json:"transformations"`
	PublicLayers         []domain.PublicLayer        `json:"publicLayers"`
	SubmittedBy          string                      `json:"submittedBy"`
	ExpectedVersion      int64                       `json:"expectedVersion"`
	SupersedesRevisionID string                      `json:"supersedesRevisionId,omitempty"`
	RemediationMappings  []domain.RemediationMapping `json:"remediationMappings,omitempty"`
}

type PreviewRevisionCommand struct {
	BaseDigest      string                  `json:"baseDigest"`
	Transformations []domain.Transformation `json:"transformations"`
	PublicLayers    []domain.PublicLayer    `json:"publicLayers"`
}

type ReviewDecision struct {
	FindingID               string               `json:"findingId"`
	Status                  domain.FindingStatus `json:"status"`
	Resolution              string               `json:"resolution"`
	ExpectedDecisionVersion *int                 `json:"expectedDecisionVersion,omitempty"`
	CorrectionReason        string               `json:"correctionReason,omitempty"`
}

type SaveDecisionsCommand struct {
	RevisionID      string           `json:"revisionId"`
	Reviewer        string           `json:"reviewer"`
	Decisions       []ReviewDecision `json:"decisions"`
	ExpectedVersion int64            `json:"expectedVersion"`
}

type CompleteReviewCommand struct {
	RevisionID      string           `json:"revisionId"`
	Reviewer        string           `json:"reviewer"`
	Action          string           `json:"action"`
	Note            string           `json:"note"`
	Decisions       []ReviewDecision `json:"decisions"`
	ExpectedVersion int64            `json:"expectedVersion"`
}

type FreezeCommand struct {
	IssuedBy        string `json:"issuedBy"`
	ExpectedVersion int64  `json:"expectedVersion"`
}

type SensitiveSiteView struct {
	ID                         string    `json:"id"`
	Category                   string    `json:"category"`
	ProtectionReason           string    `json:"protectionReason"`
	RecommendedPrecisionMeters float64   `json:"recommendedPrecisionMeters"`
	Revision                   int       `json:"revision"`
	RecordedBy                 string    `json:"recordedBy"`
	RecordedAt                 time.Time `json:"recordedAt"`
	SupersedesRevision         int       `json:"supersedesRevision,omitempty"`
	CorrectionReason           string    `json:"correctionReason,omitempty"`
}

type RevisionView struct {
	ID                   string                      `json:"id"`
	Sequence             int                         `json:"sequence"`
	BaseDigest           string                      `json:"baseDigest"`
	ContentDigest        string                      `json:"contentDigest"`
	Transformations      []domain.Transformation     `json:"transformations"`
	PublicLayerNames     []string                    `json:"publicLayerNames"`
	PublicFeatureCount   int                         `json:"publicFeatureCount"`
	SubmittedBy          string                      `json:"submittedBy"`
	SubmittedAt          time.Time                   `json:"submittedAt"`
	SupersedesRevisionID string                      `json:"supersedesRevisionId,omitempty"`
	RemediationMappings  []domain.RemediationMapping `json:"remediationMappings,omitempty"`
	RemediationResults   []domain.RemediationResult  `json:"remediationResults,omitempty"`
}

type PackageView struct {
	ID                        string                    `json:"id"`
	CaveName                  string                    `json:"caveName"`
	SurveyBounds              domain.Bounds             `json:"surveyBounds"`
	CoordinateReferenceSystem string                    `json:"coordinateReferenceSystem"`
	LayerSummaries            []domain.LayerSummary     `json:"layerSummaries"`
	Owner                     string                    `json:"owner"`
	Status                    domain.PackageStatus      `json:"status"`
	Version                   int64                     `json:"version"`
	BaseDigest                string                    `json:"baseDigest"`
	CreatedAt                 time.Time                 `json:"createdAt"`
	UpdatedAt                 time.Time                 `json:"updatedAt"`
	SensitiveSites            []SensitiveSiteView       `json:"sensitiveSites"`
	RedactionRevisions        []RevisionView            `json:"redactionRevisions"`
	Findings                  []domain.ReviewFinding    `json:"findings"`
	Credential                *domain.ReleaseCredential `json:"credential,omitempty"`
	ReleaseManifest           *domain.ReleaseManifest   `json:"releaseManifest,omitempty"`
	Audit                     []domain.AuditEvent       `json:"audit"`
	ReviewProgress            *ReviewProgress           `json:"reviewProgress,omitempty"`
}

type ReviewProgress struct {
	RevisionID        string `json:"revisionId"`
	TotalCount        int    `json:"totalCount"`
	UndecidedCount    int    `json:"undecidedCount"`
	ResolvedCount     int    `json:"resolvedCount"`
	RiskAcceptedCount int    `json:"riskAcceptedCount"`
}

type MutationResult struct {
	Package PackageView `json:"package"`
}

type FreezeResult struct {
	Package         PackageView              `json:"package"`
	Credential      domain.ReleaseCredential `json:"credential"`
	ReleaseManifest domain.ReleaseManifest   `json:"releaseManifest"`
}

type CredentialView struct {
	domain.ReleaseCredential
	ReleaseManifest domain.ReleaseManifest `json:"releaseManifest"`
}

type VerifyCredentialCommand struct {
	PackageID        string `json:"packageId"`
	RevisionID       string `json:"revisionId,omitempty"`
	ContentDigest    string `json:"contentDigest,omitempty"`
	PolicyDigest     string `json:"policyDigest,omitempty"`
	ManifestDigest   string `json:"manifestDigest,omitempty"`
	VerificationHash string `json:"verificationHash"`
}

type DigestCheck struct {
	Match bool `json:"match"`
}
type VerificationChecks struct {
	Revision DigestCheck `json:"revision"`
	Content  DigestCheck `json:"content"`
	Policy   DigestCheck `json:"policy"`
	Manifest DigestCheck `json:"manifest"`
	Hash     DigestCheck `json:"hash"`
	Storage  DigestCheck `json:"storage"`
}
type VerifyCredentialResult struct {
	Valid      bool               `json:"valid"`
	ReasonCode string             `json:"reasonCode"`
	Checks     VerificationChecks `json:"checks"`
}

type HealthView struct {
	Status        string    `json:"status"`
	CheckedAt     time.Time `json:"checkedAt"`
	SchemaVersion int       `json:"schemaVersion"`
	EventSequence int64     `json:"eventSequence"`
	PackageCount  int       `json:"packageCount"`
	Integrity     string    `json:"integrity"`
}

type CheckReportView struct {
	PackageID        string                 `json:"packageId"`
	RevisionID       string                 `json:"revisionId,omitempty"`
	Status           domain.PackageStatus   `json:"status"`
	Findings         []domain.ReviewFinding `json:"findings"`
	OpenCount        int                    `json:"openCount"`
	ResolvedCount    int                    `json:"resolvedCount"`
	AcceptedCount    int                    `json:"acceptedCount"`
	EligibleToFreeze bool                   `json:"eligibleToFreeze"`
}

func (s *Service) CreatePackage(cmd CreatePackageCommand, idempotencyKey string) (MutationResult, error) {
	scope := "create:" + idempotencyKey
	if result, ok := loadResult[MutationResult](s.store, scope); ok {
		return result, nil
	}
	if cmd.ExpectedVersion != 0 {
		return MutationResult{}, domain.Invalid("expectedVersion", "创建成果包时必须为 0")
	}
	unlock := s.locks.lock(scope)
	defer unlock()
	if result, ok := loadResult[MutationResult](s.store, scope); ok {
		return result, nil
	}
	id := s.ids("pkg")
	p, err := domain.CreatePackage(domain.NewPackage{ID: id, CaveName: cmd.CaveName, SurveyBounds: cmd.SurveyBounds, CoordinateReferenceSystem: cmd.CoordinateReferenceSystem, LayerSummaries: cmd.LayerSummaries, Owner: cmd.Owner}, s.now())
	if err != nil {
		return MutationResult{}, err
	}
	result := MutationResult{Package: packageView(p)}
	if err := s.commit(id, 0, "package.created", p, scope, result); err != nil {
		return MutationResult{}, err
	}
	return result, nil
}

func (s *Service) AddSensitiveSite(packageID string, cmd AddSensitiveSiteCommand, idempotencyKey string) (MutationResult, error) {
	scope := "site:" + packageID + ":" + idempotencyKey
	return s.mutate(packageID, cmd.ExpectedVersion, scope, "sensitive_site.recorded", func(p *domain.SurveyPackage) error {
		return p.AddSensitiveSite(domain.SensitiveSite{ID: s.ids("site"), Category: cmd.Category, OriginalCoordinate: cmd.OriginalCoordinate, ProtectionReason: cmd.ProtectionReason, RecommendedPrecisionMeters: cmd.RecommendedPrecisionMeters, RecordedBy: cmd.RecordedBy}, s.now())
	})
}

func (s *Service) ReviseMetadata(packageID string, cmd ReviseMetadataCommand, idempotencyKey string) (MutationResult, error) {
	scope := "metadata:" + packageID + ":" + idempotencyKey
	return s.mutate(packageID, cmd.ExpectedVersion, scope, "package.metadata_revised", func(p *domain.SurveyPackage) error {
		if cmd.Actor != "" && cmd.RevisedBy != "" && cmd.Actor != cmd.RevisedBy {
			return domain.Invalid("revisedBy", "与 actor 不得冲突")
		}
		actor := cmd.RevisedBy
		if actor == "" {
			actor = cmd.Actor
		}
		return p.ReviseMetadata(cmd.CaveName, cmd.SurveyBounds, cmd.CoordinateReferenceSystem, cmd.LayerSummaries, cmd.Owner, actor, cmd.RevisionReason, s.now())
	})
}

func (s *Service) ReviseSensitiveSite(packageID, siteID string, cmd ReviseSensitiveSiteCommand, idempotencyKey string) (MutationResult, error) {
	scope := "site-revision:" + packageID + ":" + siteID + ":" + idempotencyKey
	return s.mutate(packageID, cmd.ExpectedVersion, scope, "sensitive_site.revised", func(p *domain.SurveyPackage) error {
		return p.ReviseSensitiveSite(siteID, cmd.SupersedesRevision, domain.SensitiveSite{Category: cmd.Category, OriginalCoordinate: cmd.OriginalCoordinate, ProtectionReason: cmd.ProtectionReason, RecommendedPrecisionMeters: cmd.RecommendedPrecisionMeters, RecordedBy: cmd.RecordedBy}, cmd.CorrectionReason, s.now())
	})
}

func (s *Service) SubmitRevision(packageID string, cmd SubmitRevisionCommand, idempotencyKey string) (MutationResult, error) {
	scope := "revision:" + packageID + ":" + idempotencyKey
	return s.mutate(packageID, cmd.ExpectedVersion, scope, "redaction.checked", func(p *domain.SurveyPackage) error {
		preview, err := domain.PreviewTransformations(cmd.PublicLayers, p.SensitiveSites, cmd.Transformations)
		if err != nil {
			return err
		}
		revision := domain.RedactionRevision{ID: s.ids("rev"), BaseDigest: cmd.BaseDigest, Transformations: cmd.Transformations, TransformationImpacts: preview.TransformationImpacts, PublicLayers: preview.PublicLayers, SubmittedBy: cmd.SubmittedBy, SupersedesRevisionID: cmd.SupersedesRevisionID, RemediationMappings: cmd.RemediationMappings}
		if err := p.SubmitRevision(revision, s.now()); err != nil {
			return err
		}
		latest := p.LatestRevision()
		if latest.ContentDigest != preview.ContentDigest {
			return domain.Conflict("候选公开版摘要与预演结果不匹配")
		}
		findings := domain.RunChecks(p, latest, nil)
		return p.CompleteChecks(latest.ID, "automatic-checker", findings, s.now())
	})
}

func (s *Service) PreviewRevision(packageID string, cmd PreviewRevisionCommand) (domain.TransformationPreview, error) {
	p, err := s.loadPackage(packageID)
	if err != nil {
		return domain.TransformationPreview{}, err
	}
	if cmd.BaseDigest != domain.BaseDigest(p) {
		return domain.TransformationPreview{}, domain.Conflict("baseDigest 与当前内部成果摘要不匹配")
	}
	return domain.PreviewTransformations(cmd.PublicLayers, p.SensitiveSites, cmd.Transformations)
}

func decisionsFromRequest(input []ReviewDecision) (map[string]domain.FindingDecision, error) {
	decisions := make(map[string]domain.FindingDecision, len(input))
	for _, decision := range input {
		if decision.FindingID == "" {
			return nil, domain.Invalid("findingId", "不能为空")
		}
		if _, exists := decisions[decision.FindingID]; exists {
			return nil, domain.Invalid("decisions", "findingId 不得重复")
		}
		decisions[decision.FindingID] = domain.FindingDecision{Status: decision.Status, Resolution: decision.Resolution, ExpectedDecisionVersion: decision.ExpectedDecisionVersion, CorrectionReason: decision.CorrectionReason}
	}
	return decisions, nil
}

func (s *Service) SaveReviewDecisions(packageID string, cmd SaveDecisionsCommand, idempotencyKey string) (MutationResult, error) {
	scope := "review-decisions:" + packageID + ":" + idempotencyKey
	return s.mutate(packageID, cmd.ExpectedVersion, scope, "review.decisions_saved", func(p *domain.SurveyPackage) error {
		decisions, err := decisionsFromRequest(cmd.Decisions)
		if err != nil {
			return err
		}
		if len(decisions) == 0 {
			return domain.Invalid("decisions", "至少包含一项裁决")
		}
		return p.DecideFindings(cmd.RevisionID, cmd.Reviewer, decisions, s.now())
	})
}

func (s *Service) CompleteReview(packageID string, cmd CompleteReviewCommand, idempotencyKey string) (MutationResult, error) {
	scope := "review:" + packageID + ":" + idempotencyKey
	return s.mutate(packageID, cmd.ExpectedVersion, scope, "review.completed", func(p *domain.SurveyPackage) error {
		decisions, err := decisionsFromRequest(cmd.Decisions)
		if err != nil {
			return err
		}
		if len(decisions) > 0 {
			if err := p.DecideFindingsForCompletion(cmd.RevisionID, cmd.Reviewer, decisions, s.now()); err != nil {
				return err
			}
		}
		return p.FinishReview(cmd.RevisionID, cmd.Reviewer, cmd.Action, cmd.Note, s.now())
	})
}

func (s *Service) Freeze(packageID string, cmd FreezeCommand, idempotencyKey string) (FreezeResult, error) {
	scope := "freeze:" + packageID + ":" + idempotencyKey
	if result, ok := loadResult[FreezeResult](s.store, scope); ok {
		return result, nil
	}
	unlock := s.locks.lock(packageID)
	defer unlock()
	if result, ok := loadResult[FreezeResult](s.store, scope); ok {
		return result, nil
	}
	p, err := s.store.Get(packageID)
	if err != nil {
		return FreezeResult{}, err
	}
	if p.Version != cmd.ExpectedVersion {
		return FreezeResult{}, repository.ErrVersionConflict
	}
	credential, err := p.FreezeAndIssue(s.ids("cred"), cmd.IssuedBy, s.now())
	if err != nil {
		return FreezeResult{}, err
	}
	result := FreezeResult{Package: packageView(p), Credential: *credential, ReleaseManifest: *p.ReleaseManifest}
	if err := s.commit(packageID, cmd.ExpectedVersion, "release.published", p, scope, result); err != nil {
		return FreezeResult{}, err
	}
	return result, nil
}

func (s *Service) GetPackage(packageID string) (PackageView, error) {
	p, err := s.loadPackage(packageID)
	if err != nil {
		return PackageView{}, err
	}
	return packageView(p), nil
}

func (s *Service) GetFindings(packageID string) ([]domain.ReviewFinding, error) {
	p, err := s.loadPackage(packageID)
	if err != nil {
		return nil, err
	}
	return append([]domain.ReviewFinding(nil), p.Findings...), nil
}

func (s *Service) GetCheckReport(packageID string) (CheckReportView, error) {
	p, err := s.loadPackage(packageID)
	if err != nil {
		return CheckReportView{}, err
	}
	report := CheckReportView{PackageID: p.ID, Status: p.Status}
	latest := p.LatestRevision()
	if latest == nil {
		return report, nil
	}
	report.RevisionID = latest.ID
	for _, finding := range p.Findings {
		if finding.RevisionID != latest.ID {
			continue
		}
		report.Findings = append(report.Findings, finding)
		switch finding.Status {
		case domain.FindingOpen:
			report.OpenCount++
		case domain.FindingResolved:
			report.ResolvedCount++
		case domain.FindingAccepted:
			report.AcceptedCount++
		}
	}
	report.EligibleToFreeze = p.Status == domain.StatusApproved && report.OpenCount == 0 && latest.ContentDigest == domain.StableDigest(latest.PublicLayers)
	return report, nil
}

func (s *Service) GetSensitiveSites(packageID string) ([]SensitiveSiteView, error) {
	p, err := s.loadPackage(packageID)
	if err != nil {
		return nil, err
	}
	return packageView(p).SensitiveSites, nil
}

func (s *Service) GetSensitiveSiteHistory(packageID, siteID string) ([]SensitiveSiteView, error) {
	p, err := s.loadPackage(packageID)
	if err != nil {
		return nil, err
	}
	history := p.SensitiveSiteHistory[siteID]
	if len(history) == 0 {
		for _, site := range p.SensitiveSites {
			if site.ID == siteID {
				history = []domain.SensitiveSite{site}
				break
			}
		}
	}
	if len(history) == 0 {
		return nil, domain.NotFound("敏感点位 " + siteID)
	}
	views := make([]SensitiveSiteView, 0, len(history))
	for _, site := range history {
		views = append(views, sensitiveSiteView(site))
	}
	return views, nil
}

func (s *Service) GetRevisions(packageID string) ([]RevisionView, error) {
	p, err := s.loadPackage(packageID)
	if err != nil {
		return nil, err
	}
	return packageView(p).RedactionRevisions, nil
}

func (s *Service) Health() (HealthView, error) {
	report, err := s.store.Health()
	if err != nil {
		return HealthView{Status: "degraded", CheckedAt: s.now().UTC()}, err
	}
	return HealthView{Status: "ok", CheckedAt: s.now().UTC(), SchemaVersion: report.SchemaVersion, EventSequence: report.EventSequence, PackageCount: report.PackageCount, Integrity: report.Integrity}, nil
}

func (s *Service) GetCredential(packageID string) (*CredentialView, error) {
	if _, err := s.store.Health(); err != nil {
		return nil, ErrStorageIntegrity
	}
	p, err := s.loadPackage(packageID)
	if err != nil {
		return nil, err
	}
	if p.Credential == nil || p.ReleaseManifest == nil {
		return nil, domain.NotFound("发布凭据")
	}
	rev, ok := p.Revision(p.Credential.RevisionID)
	recomputedManifest := domain.ReleaseManifest{}
	if ok {
		recomputedManifest = p.BuildReleaseManifest(rev, p.Credential.IssuedAt)
	}
	if !ok || rev.ContentDigest != domain.StableDigest(rev.PublicLayers) || p.PolicyDigest(rev.ID) != p.Credential.PolicyDigest || recomputedManifest.ManifestDigest != p.Credential.ManifestDigest || p.ReleaseManifest.ManifestDigest != domain.ManifestDigest(*p.ReleaseManifest) || p.ReleaseManifest.ManifestDigest != p.Credential.ManifestDigest || !p.VerifyCredential(p.Credential.VerificationHash) {
		return nil, ErrStorageIntegrity
	}
	return &CredentialView{ReleaseCredential: *p.Credential, ReleaseManifest: *p.ReleaseManifest}, nil
}

func (s *Service) VerifyCredential(packageID, verificationHash string) (bool, error) {
	p, err := s.loadPackage(packageID)
	if err != nil {
		return false, err
	}
	return p.VerifyCredential(verificationHash), nil
}

var ErrStorageIntegrity = errors.New("存储完整性检查失败")

func (s *Service) DiagnoseCredential(cmd VerifyCredentialCommand) (VerifyCredentialResult, error) {
	if _, err := s.store.Health(); err != nil {
		return VerifyCredentialResult{}, ErrStorageIntegrity
	}
	p, err := s.loadPackage(cmd.PackageID)
	if err != nil {
		return VerifyCredentialResult{}, err
	}
	result := VerifyCredentialResult{}
	result.Checks.Storage.Match = true
	if p.Credential == nil || p.ReleaseManifest == nil {
		result.ReasonCode = "credential_not_found"
		return result, nil
	}
	credential := p.Credential
	revision, ok := p.Revision(credential.RevisionID)
	result.Checks.Revision.Match = ok && (cmd.RevisionID == "" || cmd.RevisionID == credential.RevisionID)
	if ok {
		recomputedContent := domain.StableDigest(revision.PublicLayers)
		result.Checks.Content.Match = recomputedContent == credential.ContentDigest && (cmd.ContentDigest == "" || cmd.ContentDigest == credential.ContentDigest)
		recomputedPolicy := p.PolicyDigest(revision.ID)
		result.Checks.Policy.Match = recomputedPolicy == credential.PolicyDigest && (cmd.PolicyDigest == "" || cmd.PolicyDigest == credential.PolicyDigest)
		recomputedManifest := p.BuildReleaseManifest(revision, credential.IssuedAt)
		result.Checks.Manifest.Match = recomputedManifest.ManifestDigest == credential.ManifestDigest && p.ReleaseManifest.ManifestDigest == domain.ManifestDigest(*p.ReleaseManifest) && p.ReleaseManifest.ManifestDigest == credential.ManifestDigest && (cmd.ManifestDigest == "" || cmd.ManifestDigest == credential.ManifestDigest)
	}
	result.Checks.Hash.Match = p.VerifyCredential(cmd.VerificationHash)
	result.Valid = result.Checks.Revision.Match && result.Checks.Content.Match && result.Checks.Policy.Match && result.Checks.Manifest.Match && result.Checks.Hash.Match && result.Checks.Storage.Match
	switch {
	case !result.Checks.Revision.Match:
		result.ReasonCode = "revision_mismatch"
	case !result.Checks.Content.Match:
		result.ReasonCode = "content_digest_mismatch"
	case !result.Checks.Policy.Match:
		result.ReasonCode = "policy_digest_mismatch"
	case !result.Checks.Manifest.Match:
		result.ReasonCode = "manifest_digest_mismatch"
	case !result.Checks.Hash.Match:
		result.ReasonCode = "verification_hash_mismatch"
	default:
		result.ReasonCode = "valid"
	}
	return result, nil
}

func (s *Service) mutate(packageID string, expectedVersion int64, scope, eventType string, mutation func(*domain.SurveyPackage) error) (MutationResult, error) {
	if result, ok := loadResult[MutationResult](s.store, scope); ok {
		return result, nil
	}
	unlock := s.locks.lock(packageID)
	defer unlock()
	if result, ok := loadResult[MutationResult](s.store, scope); ok {
		return result, nil
	}
	p, err := s.store.Get(packageID)
	if err != nil {
		return MutationResult{}, err
	}
	if p.Version != expectedVersion {
		return MutationResult{}, repository.ErrVersionConflict
	}
	if err := mutation(p); err != nil {
		return MutationResult{}, err
	}
	result := MutationResult{Package: packageView(p)}
	if err := s.commit(packageID, expectedVersion, eventType, p, scope, result); err != nil {
		return MutationResult{}, err
	}
	return result, nil
}

func (s *Service) commit(packageID string, expected int64, eventType string, p *domain.SurveyPackage, scope string, result any) error {
	b, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if err := s.store.Commit(packageID, expected, eventType, p, scope, b); err != nil {
		return err
	}
	return nil
}

func (s *Service) loadPackage(packageID string) (*domain.SurveyPackage, error) {
	p, err := s.store.Get(packageID)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func loadResult[T any](store repository.Store, key string) (T, bool) {
	var result T
	raw, ok := store.GetIdempotency(key)
	if !ok {
		return result, false
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, false
	}
	return result, true
}

func packageView(p *domain.SurveyPackage) PackageView {
	view := PackageView{ID: p.ID, CaveName: p.CaveName, SurveyBounds: p.SurveyBounds, CoordinateReferenceSystem: p.CoordinateReferenceSystem, LayerSummaries: append([]domain.LayerSummary(nil), p.LayerSummaries...), Owner: p.Owner, Status: p.Status, Version: p.Version, BaseDigest: domain.BaseDigest(p), CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, Findings: append([]domain.ReviewFinding(nil), p.Findings...), Audit: append([]domain.AuditEvent(nil), p.Audit...)}
	for _, site := range p.SensitiveSites {
		view.SensitiveSites = append(view.SensitiveSites, sensitiveSiteView(site))
	}
	for _, revision := range p.RedactionRevisions {
		rv := RevisionView{ID: revision.ID, Sequence: revision.Sequence, BaseDigest: revision.BaseDigest, ContentDigest: revision.ContentDigest, Transformations: append([]domain.Transformation(nil), revision.Transformations...), SubmittedBy: revision.SubmittedBy, SubmittedAt: revision.SubmittedAt, SupersedesRevisionID: revision.SupersedesRevisionID, RemediationMappings: append([]domain.RemediationMapping(nil), revision.RemediationMappings...), RemediationResults: append([]domain.RemediationResult(nil), revision.RemediationResults...)}
		for _, layer := range revision.PublicLayers {
			rv.PublicLayerNames = append(rv.PublicLayerNames, layer.Name)
			rv.PublicFeatureCount += len(layer.Features)
		}
		view.RedactionRevisions = append(view.RedactionRevisions, rv)
	}
	if p.Credential != nil {
		copyCredential := *p.Credential
		view.Credential = &copyCredential
	}
	if p.ReleaseManifest != nil {
		copyManifest := *p.ReleaseManifest
		view.ReleaseManifest = &copyManifest
	}
	if latest := p.LatestRevision(); latest != nil {
		progress := &ReviewProgress{RevisionID: latest.ID}
		for _, finding := range p.Findings {
			if finding.RevisionID != latest.ID {
				continue
			}
			progress.TotalCount++
			switch finding.Status {
			case domain.FindingOpen:
				progress.UndecidedCount++
			case domain.FindingResolved:
				progress.ResolvedCount++
			case domain.FindingAccepted:
				progress.RiskAcceptedCount++
			}
		}
		view.ReviewProgress = progress
	}
	return view
}

func sensitiveSiteView(site domain.SensitiveSite) SensitiveSiteView {
	return SensitiveSiteView{ID: site.ID, Category: site.Category, ProtectionReason: site.ProtectionReason, RecommendedPrecisionMeters: site.RecommendedPrecisionMeters, Revision: site.Revision, RecordedBy: site.RecordedBy, RecordedAt: site.RecordedAt, SupersedesRevision: site.SupersedesRevision, CorrectionReason: site.CorrectionReason}
}

func randomID(prefix string) string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("生成随机标识失败: %v", err))
	}
	return prefix + "_" + hex.EncodeToString(b)
}

func IsVersionConflict(err error) bool { return errors.Is(err, repository.ErrVersionConflict) }

type lockEntry struct {
	mu   sync.Mutex
	refs int
}
type keyedLocks struct {
	mu      sync.Mutex
	entries map[string]*lockEntry
}

func newKeyedLocks() *keyedLocks { return &keyedLocks{entries: map[string]*lockEntry{}} }

func (k *keyedLocks) lock(key string) func() {
	k.mu.Lock()
	entry := k.entries[key]
	if entry == nil {
		entry = &lockEntry{}
		k.entries[key] = entry
	}
	entry.refs++
	k.mu.Unlock()
	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		k.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(k.entries, key)
		}
		k.mu.Unlock()
	}
}
