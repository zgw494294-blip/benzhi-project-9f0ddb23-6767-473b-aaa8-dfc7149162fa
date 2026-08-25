package domain

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

type NewPackage struct {
	ID                        string
	CaveName                  string
	SurveyBounds              Bounds
	CoordinateReferenceSystem string
	LayerSummaries            []LayerSummary
	Owner                     string
}

func CreatePackage(in NewPackage, now time.Time) (*SurveyPackage, error) {
	if strings.TrimSpace(in.ID) == "" {
		return nil, Invalid("id", "不能为空")
	}
	if err := validateMetadata(in.CaveName, in.SurveyBounds, in.CoordinateReferenceSystem, in.LayerSummaries, in.Owner); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.CaveName) == "" || strings.TrimSpace(in.Owner) == "" {
		return nil, Invalid("package", "id、caveName 和 owner 不能为空")
	}
	p := &SurveyPackage{
		ID: in.ID, CaveName: strings.TrimSpace(in.CaveName), SurveyBounds: in.SurveyBounds,
		CoordinateReferenceSystem: strings.TrimSpace(in.CoordinateReferenceSystem),
		LayerSummaries:            append([]LayerSummary(nil), in.LayerSummaries...), Owner: strings.TrimSpace(in.Owner),
		Status: StatusDraft, Version: 1, CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	}
	p.recordAudit("package.created", in.Owner, "成果包已建档", "", StatusDraft, now)
	return p, nil
}

func validateMetadata(caveName string, bounds Bounds, crs string, layers []LayerSummary, owner string) error {
	if strings.TrimSpace(caveName) == "" {
		return Invalid("caveName", "不能为空")
	}
	if strings.TrimSpace(owner) == "" {
		return Invalid("owner", "不能为空")
	}
	if strings.TrimSpace(crs) == "" {
		return Invalid("coordinateReferenceSystem", "不能为空")
	}
	if bounds.MinX >= bounds.MaxX || bounds.MinY >= bounds.MaxY {
		return Invalid("surveyBounds", "边界最小值必须小于最大值")
	}
	if len(layers) == 0 {
		return Invalid("layerSummaries", "至少登记一个图层")
	}
	seen := map[string]bool{}
	for _, layer := range layers {
		name := strings.TrimSpace(layer.Name)
		if name == "" || layer.FeatureCount < 0 || seen[name] {
			return Invalid("layerSummaries", "图层名称须唯一且要素数不能为负")
		}
		seen[name] = true
	}
	return nil
}

func (p *SurveyPackage) ReviseMetadata(caveName string, bounds Bounds, crs string, layers []LayerSummary, owner, actor, reason string, now time.Time) error {
	if p.Status != StatusDraft && p.Status != StatusReturned {
		return Conflict("当前状态不允许修订成果包元数据")
	}
	if strings.TrimSpace(actor) == "" {
		return Invalid("actor", "不能为空")
	}
	if strings.TrimSpace(reason) == "" {
		return Invalid("revisionReason", "不能为空")
	}
	if err := validateMetadata(caveName, bounds, crs, layers, owner); err != nil {
		return err
	}
	for _, site := range p.SensitiveSites {
		if !bounds.Contains(site.OriginalCoordinate) {
			return Invalid("surveyBounds", "新边界排除了现有敏感点位 "+site.ID)
		}
	}
	before := StableDigest(struct {
		CaveName string
		Bounds   Bounds
		CRS      string
		Layers   []LayerSummary
		Owner    string
	}{p.CaveName, p.SurveyBounds, p.CoordinateReferenceSystem, p.LayerSummaries, p.Owner})
	p.CaveName, p.SurveyBounds, p.CoordinateReferenceSystem, p.LayerSummaries, p.Owner = strings.TrimSpace(caveName), bounds, strings.TrimSpace(crs), append([]LayerSummary(nil), layers...), strings.TrimSpace(owner)
	after := StableDigest(struct {
		CaveName string
		Bounds   Bounds
		CRS      string
		Layers   []LayerSummary
		Owner    string
	}{p.CaveName, p.SurveyBounds, p.CoordinateReferenceSystem, p.LayerSummaries, p.Owner})
	p.bump(now)
	p.recordAudit("package.metadata_revised", actor, fmt.Sprintf("修订说明：%s；字段摘要：%s -> %s", strings.TrimSpace(reason), before, after), p.Status, p.Status, now)
	return nil
}

func (p *SurveyPackage) AddSensitiveSite(site SensitiveSite, now time.Time) error {
	if p.Status != StatusDraft && p.Status != StatusReturned {
		return Conflict("当前状态不允许登记敏感点位")
	}
	if strings.TrimSpace(site.ID) == "" || strings.TrimSpace(site.Category) == "" || strings.TrimSpace(site.ProtectionReason) == "" || strings.TrimSpace(site.RecordedBy) == "" {
		return Invalid("sensitiveSite", "id、category、protectionReason 和 recordedBy 不能为空")
	}
	if site.RecommendedPrecisionMeters <= 0 {
		return Invalid("recommendedPrecisionMeters", "必须大于零")
	}
	if !p.SurveyBounds.Contains(site.OriginalCoordinate) {
		return Invalid("originalCoordinate", "必须位于成果包测绘范围内")
	}
	for _, existing := range p.SensitiveSites {
		if existing.ID == site.ID {
			return Conflict("敏感点位 id 已存在；修订记录不可覆盖")
		}
	}
	site.PackageID = p.ID
	site.Revision = 1
	site.RecordedAt = now.UTC()
	p.SensitiveSites = append(p.SensitiveSites, site)
	if p.SensitiveSiteHistory == nil {
		p.SensitiveSiteHistory = map[string][]SensitiveSite{}
	}
	p.SensitiveSiteHistory[site.ID] = append(p.SensitiveSiteHistory[site.ID], site)
	p.bump(now)
	p.recordAudit("sensitive_site.recorded", site.RecordedBy, "已登记不可变敏感点位修订", p.Status, p.Status, now)
	return nil
}

func (p *SurveyPackage) ReviseSensitiveSite(siteID string, supersedes int, site SensitiveSite, correctionReason string, now time.Time) error {
	if p.Status != StatusDraft && p.Status != StatusReturned {
		return Conflict("当前状态不允许更正敏感点位")
	}
	if strings.TrimSpace(correctionReason) == "" {
		return Invalid("correctionReason", "不能为空")
	}
	index := -1
	for i := range p.SensitiveSites {
		if p.SensitiveSites[i].ID == siteID {
			index = i
			break
		}
	}
	if index < 0 {
		return NotFound("敏感点位 " + siteID)
	}
	current := p.SensitiveSites[index]
	if supersedes != current.Revision {
		return Conflict("被替代修订已不是当前生效修订")
	}
	if strings.TrimSpace(site.Category) == "" || strings.TrimSpace(site.ProtectionReason) == "" || strings.TrimSpace(site.RecordedBy) == "" {
		return Invalid("sensitiveSite", "category、protectionReason 和 recordedBy 不能为空")
	}
	if site.RecommendedPrecisionMeters <= 0 {
		return Invalid("recommendedPrecisionMeters", "必须大于零")
	}
	if !p.SurveyBounds.Contains(site.OriginalCoordinate) {
		return Invalid("originalCoordinate", "必须位于成果包测绘范围内")
	}
	site.ID, site.PackageID, site.Revision = current.ID, p.ID, current.Revision+1
	site.SupersedesRevision, site.CorrectionReason, site.RecordedAt = current.Revision, strings.TrimSpace(correctionReason), now.UTC()
	if p.SensitiveSiteHistory == nil {
		p.SensitiveSiteHistory = map[string][]SensitiveSite{}
	}
	if len(p.SensitiveSiteHistory[siteID]) == 0 {
		p.SensitiveSiteHistory[siteID] = append(p.SensitiveSiteHistory[siteID], current)
	}
	p.SensitiveSiteHistory[siteID] = append(p.SensitiveSiteHistory[siteID], site)
	p.SensitiveSites[index] = site
	p.bump(now)
	p.recordAudit("sensitive_site.revised", site.RecordedBy, fmt.Sprintf("点位 %s 修订 %d 替代 %d；更正说明：%s", siteID, site.Revision, supersedes, site.CorrectionReason), p.Status, p.Status, now)
	return nil
}

func (p *SurveyPackage) SubmitRevision(revision RedactionRevision, now time.Time) error {
	if p.Status != StatusDraft && p.Status != StatusReturned {
		return Conflict("当前状态不允许提交脱敏修订")
	}
	if len(p.SensitiveSites) == 0 {
		return Conflict("至少登记一个敏感点位后才能提交脱敏修订")
	}
	if strings.TrimSpace(revision.ID) == "" || strings.TrimSpace(revision.SubmittedBy) == "" {
		return Invalid("redactionRevision", "id 和 submittedBy 不能为空")
	}
	if revision.BaseDigest != BaseDigest(p) {
		return Conflict("baseDigest 与当前内部成果摘要不匹配")
	}
	for _, existing := range p.RedactionRevisions {
		if existing.ID == revision.ID {
			return Conflict("脱敏修订 id 已存在")
		}
	}
	if p.Status == StatusReturned {
		if err := p.validateRemediation(&revision); err != nil {
			return err
		}
	} else if revision.SupersedesRevisionID != "" || len(revision.RemediationMappings) != 0 {
		return Invalid("supersedesRevisionId", "首次提交不得声明整改继承关系")
	}
	from := p.Status
	revision.PackageID = p.ID
	revision.Sequence = len(p.RedactionRevisions) + 1
	revision.SubmittedAt = now.UTC()
	revision.ContentDigest = StableDigest(revision.PublicLayers)
	p.RedactionRevisions = append(p.RedactionRevisions, revision)
	p.Status = StatusPendingCheck
	p.bump(now)
	p.recordAudit("redaction.submitted", revision.SubmittedBy, "候选公开版进入自动检查", from, StatusPendingCheck, now)
	return nil
}

func (p *SurveyPackage) validateRemediation(revision *RedactionRevision) error {
	latest := p.LatestRevision()
	if latest == nil || revision.SupersedesRevisionID != latest.ID {
		return Invalid("supersedesRevisionId", "必须引用当前最新修订")
	}
	open := map[string]ReviewFinding{}
	for _, finding := range p.Findings {
		if finding.RevisionID == latest.ID && finding.Status == FindingOpen {
			open[finding.ID] = finding
		}
	}
	seen := map[string]bool{}
	for i, mapping := range revision.RemediationMappings {
		if _, ok := open[mapping.FindingID]; !ok {
			return Invalid(fmt.Sprintf("remediationMappings[%d].findingId", i), "不属于被替代修订的未决发现")
		}
		if seen[mapping.FindingID] {
			return Invalid("remediationMappings", "findingId 不得重复")
		}
		if strings.TrimSpace(mapping.Explanation) == "" {
			return Invalid(fmt.Sprintf("remediationMappings[%d].explanation", i), "不能为空")
		}
		if (mapping.TransformationIndex == nil) == (strings.TrimSpace(mapping.PublicLocation) == "") {
			return Invalid(fmt.Sprintf("remediationMappings[%d]", i), "必须且只能引用 transformationIndex 或 publicLocation")
		}
		if mapping.TransformationIndex != nil && (*mapping.TransformationIndex < 0 || *mapping.TransformationIndex >= len(revision.Transformations)) {
			return Invalid(fmt.Sprintf("remediationMappings[%d].transformationIndex", i), "引用的变换不存在")
		}
		if mapping.PublicLocation != "" && !PublicLocationExists(revision.PublicLayers, mapping.PublicLocation) {
			return Invalid(fmt.Sprintf("remediationMappings[%d].publicLocation", i), "引用的公开要素位置不存在")
		}
		seen[mapping.FindingID] = true
	}
	for id := range open {
		if !seen[id] {
			return Invalid("remediationMappings", "遗漏未决发现 "+id)
		}
	}
	return nil
}

func (p *SurveyPackage) CompleteChecks(revisionID, actor string, findings []ReviewFinding, now time.Time) error {
	if p.Status != StatusPendingCheck {
		return Conflict("只有待检查成果包可完成自动检查")
	}
	rev, ok := p.Revision(revisionID)
	if !ok || rev.ContentDigest != StableDigest(rev.PublicLayers) {
		return Conflict("候选公开版摘要不匹配")
	}
	for i := range findings {
		findings[i].PackageID = p.ID
		findings[i].RevisionID = revisionID
		findings[i].Status = FindingOpen
	}
	if rev.SupersedesRevisionID != "" {
		previous := map[string]ReviewFinding{}
		for _, f := range p.Findings {
			if f.RevisionID == rev.SupersedesRevisionID {
				previous[f.ID] = f
			}
		}
		for _, mapping := range rev.RemediationMappings {
			old := previous[mapping.FindingID]
			result := RemediationResult{FindingID: mapping.FindingID, Outcome: RemediationEliminated, Explanation: mapping.Explanation}
			if mapping.TransformationIndex != nil {
				tr := rev.Transformations[*mapping.TransformationIndex]
				hits := transformedFeatureHits(rev.PublicLayers, tr)
				if *mapping.TransformationIndex < len(rev.TransformationImpacts) {
					hits = rev.TransformationImpacts[*mapping.TransformationIndex].MatchedFeatureCount
				}
				if tr.Type != TransformClipLayer && hits == 0 {
					result.Outcome = RemediationUnlocatable
				}
			}
			for i := range findings {
				if findings[i].RuleCode == old.RuleCode && findings[i].Location == old.Location {
					result.Outcome, result.NewFindingID = RemediationReproduced, findings[i].ID
					break
				}
			}
			rev.RemediationResults = append(rev.RemediationResults, result)
		}
	}
	p.Findings = append(p.Findings, findings...)
	p.Status = StatusPendingReview
	p.recordAudit("checks.completed", actor, fmt.Sprintf("自动检查完成，发现 %d 项", len(findings)), StatusPendingCheck, StatusPendingReview, now)
	return nil
}

func (p *SurveyPackage) DecideFindings(revisionID, reviewer string, decisions map[string]FindingDecision, now time.Time) error {
	return p.decideFindings(revisionID, reviewer, decisions, now, true)
}

func (p *SurveyPackage) DecideFindingsForCompletion(revisionID, reviewer string, decisions map[string]FindingDecision, now time.Time) error {
	return p.decideFindings(revisionID, reviewer, decisions, now, false)
}

func (p *SurveyPackage) decideFindings(revisionID, reviewer string, decisions map[string]FindingDecision, now time.Time, incrementVersion bool) error {
	if p.Status != StatusPendingReview {
		return Conflict("只有待复核成果包可裁决发现")
	}
	if strings.TrimSpace(reviewer) == "" {
		return Invalid("reviewer", "不能为空")
	}
	if p.LatestRevision() == nil || revisionID != p.LatestRevision().ID {
		return Conflict("只能裁决最新候选版的发现")
	}
	for id, decision := range decisions {
		if decision.Status != FindingResolved && decision.Status != FindingAccepted {
			return Invalid("decision.status", "必须为 resolved 或 accepted")
		}
		if strings.TrimSpace(decision.Resolution) == "" {
			return Invalid("decision.resolution", "不能为空")
		}
		found := false
		for i := range p.Findings {
			f := &p.Findings[i]
			if f.ID == id && f.RevisionID == revisionID {
				found = true
				if f.Status != FindingOpen {
					if decision.ExpectedDecisionVersion == nil || *decision.ExpectedDecisionVersion != f.DecisionVersion {
						return Conflict("发现 " + id + " 的裁决版本已过期")
					}
					if strings.TrimSpace(decision.CorrectionReason) == "" {
						return Invalid("correctionReason", "更正已裁决发现时不能为空")
					}
				} else if decision.ExpectedDecisionVersion != nil {
					return Conflict("未裁决发现不接受裁决版本")
				}
				old := f.Status
				f.Status, f.Resolution, f.DecidedBy = decision.Status, decision.Resolution, reviewer
				t := now.UTC()
				f.DecidedAt = &t
				f.DecisionVersion++
				f.DecisionHistory = append(f.DecisionHistory, FindingDecisionRecord{Version: f.DecisionVersion, OldStatus: old, NewStatus: f.Status, Resolution: f.Resolution, CorrectionReason: decision.CorrectionReason, Reviewer: reviewer, DecidedAt: t})
			}
		}
		if !found {
			return NotFound("检查发现 " + id)
		}
	}
	if incrementVersion {
		p.bump(now)
	}
	p.recordAudit("review.decisions_saved", reviewer, fmt.Sprintf("已保存 %d 项复核裁决", len(decisions)), p.Status, p.Status, now)
	return nil
}

type FindingDecision struct {
	Status                  FindingStatus
	Resolution              string
	ExpectedDecisionVersion *int
	CorrectionReason        string
}

func (p *SurveyPackage) FinishReview(revisionID, reviewer, action, note string, now time.Time) error {
	if p.Status != StatusPendingReview {
		return Conflict("当前状态不可完成复核")
	}
	if _, ok := p.Revision(revisionID); !ok || revisionID != p.LatestRevision().ID {
		return Conflict("只能复核最新候选版")
	}
	if strings.TrimSpace(reviewer) == "" {
		return Invalid("reviewer", "不能为空")
	}
	switch action {
	case "return":
		if strings.TrimSpace(note) == "" {
			return Invalid("note", "退回时必须填写退回说明")
		}
		if !p.HasOpenFindings(revisionID) {
			return Conflict("退回时必须存在仍需整改的未决发现")
		}
		p.Status = StatusReturned
		p.bump(now)
		p.recordAudit("review.returned", reviewer, note, StatusPendingReview, StatusReturned, now)
	case "approve":
		if p.HasOpenFindings(revisionID) {
			return Conflict("仍有未决发现，不能通过复核")
		}
		p.Status = StatusApproved
		p.bump(now)
		p.recordAudit("review.approved", reviewer, note, StatusPendingReview, StatusApproved, now)
	default:
		return Invalid("action", "必须为 approve 或 return")
	}
	return nil
}

func (p *SurveyPackage) FreezeAndIssue(credentialID, issuedBy string, now time.Time) (*ReleaseCredential, error) {
	if p.Status != StatusApproved {
		return nil, Conflict("只有已通过复核的候选版可以冻结")
	}
	rev := p.LatestRevision()
	if rev == nil || p.HasOpenFindings(rev.ID) || rev.ContentDigest != StableDigest(rev.PublicLayers) {
		return nil, Conflict("候选版存在未决发现或摘要不匹配")
	}
	if strings.TrimSpace(credentialID) == "" || strings.TrimSpace(issuedBy) == "" {
		return nil, Invalid("credential", "id 和 issuedBy 不能为空")
	}
	policy := p.PolicyDigest(rev.ID)
	issuedAt := now.UTC()
	manifest := p.BuildReleaseManifest(rev, issuedAt)
	verification := credentialHash(credentialID, p.ID, rev.ID, rev.ContentDigest, policy, manifest.ManifestDigest, issuedBy, issuedAt)
	cred := &ReleaseCredential{ID: credentialID, PackageID: p.ID, RevisionID: rev.ID, ContentDigest: rev.ContentDigest, PolicyDigest: policy, ManifestDigest: manifest.ManifestDigest, IssuedBy: issuedBy, IssuedAt: issuedAt, VerificationHash: verification}
	p.Status = StatusFrozen
	p.recordAudit("release.frozen", issuedBy, "公开版本已经冻结", StatusApproved, StatusFrozen, now)
	p.Credential = cred
	p.ReleaseManifest = &manifest
	p.Status = StatusPublished
	p.bump(now)
	p.recordAudit("credential.issued", issuedBy, "不可变发布凭据已经签发", StatusFrozen, StatusPublished, now)
	return cred, nil
}

func (p *SurveyPackage) VerifyCredential(hash string) bool {
	if p.Credential == nil || p.ReleaseManifest == nil {
		return false
	}
	expected := credentialHash(p.Credential.ID, p.ID, p.Credential.RevisionID, p.Credential.ContentDigest, p.Credential.PolicyDigest, p.Credential.ManifestDigest, p.Credential.IssuedBy, p.Credential.IssuedAt)
	return subtle.ConstantTimeCompare([]byte(expected), []byte(hash)) == 1 && p.ReleaseManifest.ManifestDigest == p.Credential.ManifestDigest
}

func (p *SurveyPackage) PolicyDigest(revisionID string) string {
	type policyItem struct{ Code, Status, Resolution string }
	items := []policyItem{}
	for _, f := range p.Findings {
		if f.RevisionID == revisionID {
			items = append(items, policyItem{f.RuleCode, string(f.Status), f.Resolution})
		}
	}
	sort.Slice(items, func(i, j int) bool { return fmt.Sprint(items[i]) < fmt.Sprint(items[j]) })
	revision, _ := p.Revision(revisionID)
	var transformations []Transformation
	if revision != nil {
		transformations = revision.Transformations
	}
	return StableDigest(struct {
		Transformations []Transformation
		Decisions       []policyItem
	}{transformations, items})
}

func credentialHash(id, packageID, revisionID, content, policy, manifest, actor string, issued time.Time) string {
	value := strings.Join([]string{id, packageID, revisionID, content, policy, manifest, actor, issued.UTC().Format(time.RFC3339Nano)}, "|")
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (p *SurveyPackage) BuildReleaseManifest(rev *RedactionRevision, issuedAt time.Time) ReleaseManifest {
	m := ReleaseManifest{PackageID: p.ID, RevisionID: rev.ID, IssuedAt: issuedAt.UTC()}
	for _, layer := range rev.PublicLayers {
		m.Layers = append(m.Layers, ManifestLayer{Name: layer.Name, FeatureCount: len(layer.Features), LayerDigest: StableDigest(layer)})
	}
	sort.Slice(m.Layers, func(i, j int) bool {
		if m.Layers[i].Name != m.Layers[j].Name {
			return m.Layers[i].Name < m.Layers[j].Name
		}
		return m.Layers[i].LayerDigest < m.Layers[j].LayerDigest
	})
	counts := map[TransformationType]int{}
	for _, tr := range rev.Transformations {
		counts[tr.Type]++
	}
	for kind, count := range counts {
		m.TransformationSummary = append(m.TransformationSummary, TransformationCount{Type: kind, Count: count})
	}
	sort.Slice(m.TransformationSummary, func(i, j int) bool { return m.TransformationSummary[i].Type < m.TransformationSummary[j].Type })
	for _, f := range p.Findings {
		if f.RevisionID == rev.ID {
			switch f.Status {
			case FindingOpen:
				m.DecisionSummary.Open++
			case FindingResolved:
				m.DecisionSummary.Resolved++
			case FindingAccepted:
				m.DecisionSummary.Accepted++
			}
		}
	}
	m.ManifestDigest = ManifestDigest(m)
	return m
}

func ManifestDigest(m ReleaseManifest) string { m.ManifestDigest = ""; return StableDigest(m) }

func (p *SurveyPackage) Revision(id string) (*RedactionRevision, bool) {
	for i := range p.RedactionRevisions {
		if p.RedactionRevisions[i].ID == id {
			return &p.RedactionRevisions[i], true
		}
	}
	return nil, false
}

func (p *SurveyPackage) LatestRevision() *RedactionRevision {
	if len(p.RedactionRevisions) == 0 {
		return nil
	}
	return &p.RedactionRevisions[len(p.RedactionRevisions)-1]
}

func (p *SurveyPackage) HasOpenFindings(revisionID string) bool {
	for _, f := range p.Findings {
		if f.RevisionID == revisionID && f.Status == FindingOpen {
			return true
		}
	}
	return false
}

func (p *SurveyPackage) bump(now time.Time) { p.Version++; p.UpdatedAt = now.UTC() }

func (p *SurveyPackage) recordAudit(kind, actor, detail string, from, to PackageStatus, now time.Time) {
	p.Audit = append(p.Audit, AuditEvent{Sequence: int64(len(p.Audit) + 1), Type: kind, Actor: actor, At: now.UTC(), From: from, To: to, Detail: detail})
}
