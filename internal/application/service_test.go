package application

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"karst-map-release/internal/domain"
	"karst-map-release/internal/repository"
)

func testService() *Service {
	s := NewService(repository.NewMemoryStore())
	n := 0
	s.ids = func(prefix string) string {
		n++
		return prefix + "-test-" + time.Unix(int64(n), 0).UTC().Format("150405")
	}
	s.now = func() time.Time { return time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC) }
	return s
}

func createPackageAndSite(t *testing.T, s *Service) (MutationResult, MutationResult) {
	t.Helper()
	created, err := s.CreatePackage(CreatePackageCommand{CaveName: "测试洞穴", SurveyBounds: domain.Bounds{MinX: 0, MinY: 0, MaxX: 1000, MaxY: 1000}, CoordinateReferenceSystem: "LOCAL", LayerSummaries: []domain.LayerSummary{{Name: "survey", FeatureCount: 1}}, Owner: "surveyor"}, "create")
	if err != nil {
		t.Fatal(err)
	}
	site, err := s.AddSensitiveSite(created.Package.ID, AddSensitiveSiteCommand{Category: "entrance", OriginalCoordinate: domain.Coordinate{X: 100, Y: 100}, ProtectionReason: "保护入口", RecommendedPrecisionMeters: 50, RecordedBy: "surveyor", ExpectedVersion: created.Package.Version}, "site")
	if err != nil {
		t.Fatal(err)
	}
	return created, site
}

func TestMetadataAndSensitiveSiteRevisionAreImmutableAndIdempotent(t *testing.T) {
	s := testService()
	_, withSite := createPackageAndSite(t, s)
	packageID, siteID := withSite.Package.ID, withSite.Package.SensitiveSites[0].ID
	metadata := ReviseMetadataCommand{CaveName: "新洞穴名", SurveyBounds: domain.Bounds{MinX: 0, MinY: 0, MaxX: 900, MaxY: 900}, CoordinateReferenceSystem: "EPSG:4547", LayerSummaries: []domain.LayerSummary{{Name: "survey", FeatureCount: 2}}, Owner: "owner-2", Actor: "editor", RevisionReason: "纠正底图范围", ExpectedVersion: withSite.Package.Version}
	revised, err := s.ReviseMetadata(packageID, metadata, "metadata")
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.ReviseMetadata(packageID, metadata, "metadata")
	if err != nil || domain.StableDigest(revised) != domain.StableDigest(retry) {
		t.Fatal("元数据修订幂等结果不一致")
	}
	if revised.Package.Version != withSite.Package.Version+1 || revised.Package.BaseDigest == withSite.Package.BaseDigest {
		t.Fatal("元数据修订未递增版本或重算摘要")
	}
	invalid := metadata
	invalid.ExpectedVersion, invalid.SurveyBounds = revised.Package.Version, domain.Bounds{MinX: 200, MinY: 200, MaxX: 900, MaxY: 900}
	if _, err := s.ReviseMetadata(packageID, invalid, "metadata-invalid"); err == nil {
		t.Fatal("边界不应排除已有敏感点位")
	}
	correction := ReviseSensitiveSiteCommand{Category: "entrance", OriginalCoordinate: domain.Coordinate{X: 120, Y: 120}, ProtectionReason: "调整保护要求", RecommendedPrecisionMeters: 100, RecordedBy: "editor", SupersedesRevision: 1, CorrectionReason: "初录偏差", ExpectedVersion: revised.Package.Version}
	corrected, err := s.ReviseSensitiveSite(packageID, siteID, correction, "site-revise")
	if err != nil {
		t.Fatal(err)
	}
	history, err := s.GetSensitiveSiteHistory(packageID, siteID)
	if err != nil || len(history) != 2 || history[0].Revision != 1 || history[1].Revision != 2 {
		t.Fatalf("点位历史不完整: %#v, %v", history, err)
	}
	b, _ := json.Marshal(history)
	if strings.Contains(string(b), "originalCoordinate") || strings.Contains(string(b), "120") {
		t.Fatalf("点位历史泄露原始坐标: %s", b)
	}
	correction.ExpectedVersion = corrected.Package.Version
	if _, err := s.ReviseSensitiveSite(packageID, siteID, correction, "site-stale"); err == nil {
		t.Fatal("过期被替代修订不应成功")
	}
}

func TestPreviewRemediationBatchReviewAndCredentialDiagnostics(t *testing.T) {
	s := testService()
	_, withSite := createPackageAndSite(t, s)
	packageID, siteID := withSite.Package.ID, withSite.Package.SensitiveSites[0].ID
	coord := &domain.Coordinate{X: 100, Y: 100}
	layers := []domain.PublicLayer{{Name: "survey", Features: []domain.PublicFeature{{ID: "feature-1", Name: "入口", SourceSiteID: siteID, Coordinate: coord}}}}
	preview, err := s.PreviewRevision(packageID, PreviewRevisionCommand{BaseDigest: withSite.Package.BaseDigest, PublicLayers: layers, Transformations: []domain.Transformation{{Type: domain.TransformGrid, SourceSiteID: siteID, GridMeters: 10}}})
	if err != nil || len(preview.Coverage) != 1 || len(preview.Warnings) == 0 {
		t.Fatalf("预演覆盖或告警不正确: %#v, %v", preview, err)
	}
	afterPreview, _ := s.GetPackage(packageID)
	if afterPreview.Version != withSite.Package.Version || len(afterPreview.RedactionRevisions) != 0 {
		t.Fatal("预演改变了聚合")
	}
	first, err := s.SubmitRevision(packageID, SubmitRevisionCommand{BaseDigest: withSite.Package.BaseDigest, PublicLayers: layers, SubmittedBy: "surveyor", ExpectedVersion: withSite.Package.Version}, "revision-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Package.Findings) == 0 {
		t.Fatal("应产生坐标泄露发现")
	}
	findingID, revisionID := first.Package.Findings[0].ID, first.Package.RedactionRevisions[0].ID
	returned, err := s.CompleteReview(packageID, CompleteReviewCommand{RevisionID: revisionID, Reviewer: "reviewer", Action: "return", Note: "坐标仍需移除", ExpectedVersion: first.Package.Version}, "return")
	if err != nil {
		t.Fatal(err)
	}
	index := 0
	second, err := s.SubmitRevision(packageID, SubmitRevisionCommand{BaseDigest: returned.Package.BaseDigest, PublicLayers: layers, Transformations: []domain.Transformation{{Type: domain.TransformRemoveCoordinate, SourceSiteID: siteID}}, SupersedesRevisionID: revisionID, RemediationMappings: []domain.RemediationMapping{{FindingID: findingID, Explanation: "移除公开坐标", TransformationIndex: &index}}, SubmittedBy: "surveyor", ExpectedVersion: returned.Package.Version}, "revision-2")
	if err != nil {
		t.Fatal(err)
	}
	latest := second.Package.RedactionRevisions[1]
	if latest.ContentDigest != domain.StableDigest([]domain.PublicLayer{{Name: "survey", Features: []domain.PublicFeature{{ID: "feature-1", Name: "入口", SourceSiteID: siteID}}}}) || len(latest.RemediationResults) != 1 || latest.RemediationResults[0].Outcome != domain.RemediationEliminated {
		t.Fatalf("整改闭环或摘要不正确: %#v", latest)
	}
	approved, err := s.CompleteReview(packageID, CompleteReviewCommand{RevisionID: latest.ID, Reviewer: "reviewer", Action: "approve", Note: "整改通过", ExpectedVersion: second.Package.Version}, "approve")
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := s.Freeze(packageID, FreezeCommand{IssuedBy: "publisher", ExpectedVersion: approved.Package.Version}, "freeze")
	if err != nil {
		t.Fatal(err)
	}
	if frozen.ReleaseManifest.ManifestDigest == "" || len(frozen.ReleaseManifest.Layers) != 1 {
		t.Fatal("发布清单未固化")
	}
	verified, err := s.DiagnoseCredential(VerifyCredentialCommand{PackageID: packageID, RevisionID: frozen.Credential.RevisionID, ContentDigest: frozen.Credential.ContentDigest, PolicyDigest: frozen.Credential.PolicyDigest, ManifestDigest: frozen.Credential.ManifestDigest, VerificationHash: frozen.Credential.VerificationHash})
	if err != nil || !verified.Valid || verified.ReasonCode != "valid" {
		t.Fatalf("凭据诊断失败: %#v, %v", verified, err)
	}
	verified, err = s.DiagnoseCredential(VerifyCredentialCommand{PackageID: packageID, ContentDigest: "changed", VerificationHash: frozen.Credential.VerificationHash})
	if err != nil || verified.Valid || verified.ReasonCode != "content_digest_mismatch" {
		t.Fatalf("内容摘要诊断不准确: %#v, %v", verified, err)
	}
}

func TestReviewDecisionsCanBeSavedAndCorrectedInBatches(t *testing.T) {
	s := testService()
	_, withSite := createPackageAndSite(t, s)
	packageID, siteID := withSite.Package.ID, withSite.Package.SensitiveSites[0].ID
	coord := &domain.Coordinate{X: 100, Y: 100}
	result, err := s.SubmitRevision(packageID, SubmitRevisionCommand{BaseDigest: withSite.Package.BaseDigest, PublicLayers: []domain.PublicLayer{{Name: "survey", Features: []domain.PublicFeature{{ID: "f", SourceSiteID: siteID, Coordinate: coord}, {ID: "f", SourceSiteID: siteID, Coordinate: coord}}}}, SubmittedBy: "surveyor", ExpectedVersion: withSite.Package.Version}, "batch-revision")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Package.Findings) < 2 {
		t.Fatalf("预期多个稳定检查发现: %#v", result.Package.Findings)
	}
	revisionID := result.Package.RedactionRevisions[0].ID
	firstID := result.Package.Findings[0].ID
	batch, err := s.SaveReviewDecisions(packageID, SaveDecisionsCommand{RevisionID: revisionID, Reviewer: "reviewer", Decisions: []ReviewDecision{{FindingID: firstID, Status: domain.FindingAccepted, Resolution: "接受风险"}}, ExpectedVersion: result.Package.Version}, "batch-1")
	if err != nil {
		t.Fatal(err)
	}
	if batch.Package.Status != domain.StatusPendingReview || batch.Package.Version != result.Package.Version+1 || batch.Package.ReviewProgress.RiskAcceptedCount != 1 {
		t.Fatal("分批裁决未保持待复核状态或未递增版本")
	}
	retry, err := s.SaveReviewDecisions(packageID, SaveDecisionsCommand{}, "batch-1")
	if err != nil || domain.StableDigest(batch) != domain.StableDigest(retry) {
		t.Fatal("裁决批次幂等结果不一致")
	}
	oldVersion := 1
	_, err = s.SaveReviewDecisions(packageID, SaveDecisionsCommand{RevisionID: revisionID, Reviewer: "reviewer-2", Decisions: []ReviewDecision{{FindingID: firstID, Status: domain.FindingResolved, Resolution: "重新消解", ExpectedDecisionVersion: &oldVersion, CorrectionReason: "补充证据"}}, ExpectedVersion: batch.Package.Version}, "batch-correct")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompleteReview(packageID, CompleteReviewCommand{RevisionID: revisionID, Reviewer: "reviewer", Action: "approve", ExpectedVersion: batch.Package.Version + 1}, "approve-incomplete"); err == nil {
		t.Fatal("仍有未裁决发现时不应通过")
	}
	staleVersion := 1
	if _, err := s.SaveReviewDecisions(packageID, SaveDecisionsCommand{RevisionID: revisionID, Reviewer: "reviewer-3", Decisions: []ReviewDecision{{FindingID: firstID, Status: domain.FindingAccepted, Resolution: "覆盖", ExpectedDecisionVersion: &staleVersion, CorrectionReason: "错误覆盖"}}, ExpectedVersion: batch.Package.Version + 1}, "batch-stale"); err == nil {
		t.Fatal("过期裁决版本不应覆盖当前结果")
	}
}
