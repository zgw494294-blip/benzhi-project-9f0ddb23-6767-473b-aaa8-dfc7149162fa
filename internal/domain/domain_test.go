package domain

import (
	"testing"
	"time"
)

func TestProtectionWorkflowAndFreezeGate(t *testing.T) {
	now := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)
	p, err := CreatePackage(NewPackage{ID: "pkg-1", CaveName: "测试洞穴", SurveyBounds: Bounds{MinX: 0, MinY: 0, MaxX: 10000, MaxY: 10000}, CoordinateReferenceSystem: "EPSG:4547", LayerSummaries: []LayerSummary{{Name: "entrances", FeatureCount: 1}}, Owner: "surveyor"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.AddSensitiveSite(SensitiveSite{ID: "site-1", Category: "entrance", OriginalCoordinate: Coordinate{X: 1000, Y: 2000}, ProtectionReason: "脆弱入口", RecommendedPrecisionMeters: 500, RecordedBy: "surveyor"}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	coord := &Coordinate{X: 1001, Y: 2001}
	revision := RedactionRevision{ID: "rev-1", BaseDigest: BaseDigest(p), PublicLayers: []PublicLayer{{Name: "entrances", Features: []PublicFeature{{ID: "f-1", SourceSiteID: "site-1", Coordinate: coord}}}}, SubmittedBy: "surveyor"}
	if err := p.SubmitRevision(revision, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	findings := RunChecks(p, p.LatestRevision(), func() string { return "finding-1" })
	if len(findings) != 1 || findings[0].RuleCode != "COORDINATE_LEAK" {
		t.Fatalf("预期坐标泄露发现，得到 %#v", findings)
	}
	if err := p.CompleteChecks("rev-1", "checker", findings, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := p.FinishReview("rev-1", "reviewer", "approve", "", now.Add(4*time.Minute)); err == nil {
		t.Fatal("存在未决发现时不应允许通过")
	}
	if err := p.DecideFindings("rev-1", "reviewer", map[string]FindingDecision{"finding-1": {Status: FindingAccepted, Resolution: "风险由保护负责人接受"}}, now.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := p.FinishReview("rev-1", "reviewer", "approve", "复核通过", now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	credential, err := p.FreezeAndIssue("cred-1", "publisher", now.Add(6*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != StatusPublished || !p.VerifyCredential(credential.VerificationHash) {
		t.Fatal("发布状态或凭据校验不正确")
	}
	if _, err := p.FreezeAndIssue("cred-2", "publisher", now.Add(7*time.Minute)); err == nil {
		t.Fatal("发布凭据必须不可重复签发")
	}
}

func TestApplyAllTransformationTypes(t *testing.T) {
	sites := []SensitiveSite{{ID: "s1"}}
	coord := &Coordinate{X: 126, Y: 274}
	layers := []PublicLayer{{Name: "points", Features: []PublicFeature{{ID: "f", Name: "秘密入口", SourceSiteID: "s1", Coordinate: coord}}}, {Name: "private", Features: []PublicFeature{{ID: "hidden"}}}}
	transforms := []Transformation{{Type: TransformGrid, SourceSiteID: "s1", LayerName: "points", GridMeters: 100}, {Type: TransformReplaceName, SourceSiteID: "s1", LayerName: "points", ReplacementName: "一般地貌"}, {Type: TransformClipLayer, LayerName: "private"}}
	out, err := ApplyTransformations(layers, sites, transforms)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Features[0].Name != "一般地貌" || out[0].Features[0].Coordinate.X != 100 || out[0].Features[0].Coordinate.Y != 300 {
		t.Fatalf("变换结果不正确: %#v", out)
	}
	removed, err := ApplyTransformations(out, sites, []Transformation{{Type: TransformRemoveCoordinate, SourceSiteID: "s1"}})
	if err != nil || removed[0].Features[0].Coordinate != nil {
		t.Fatal("坐标移除未生效")
	}
	if layers[0].Features[0].Name != "秘密入口" || layers[0].Features[0].Coordinate.X != 126 {
		t.Fatal("变换不应修改输入")
	}
}
