package torn_log_tail_reappend_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"karst-map-release/internal/domain"
	"karst-map-release/internal/repository"
)

func TestTornLogTailIsTruncatedBeforeLaterAppend(t *testing.T) {
	dir := t.TempDir()
	store, err := repository.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	pkg, err := domain.CreatePackage(domain.NewPackage{
		ID:                        "pkg-torn-tail",
		CaveName:                  "撕裂尾部恢复测试洞穴",
		SurveyBounds:              domain.Bounds{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100},
		CoordinateReferenceSystem: "LOCAL",
		LayerSummaries:            []domain.LayerSummary{{Name: "survey", FeatureCount: 1}},
		Owner:                     "surveyor",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Commit(pkg.ID, 0, "package.created", pkg, "create", json.RawMessage(`{"version":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	logPath := filepath.Join(dir, "events.jsonl")
	logFile, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := logFile.WriteString(`{"schemaVersion":1,"sequence":2`); err != nil {
		logFile.Close()
		t.Fatal(err)
	}
	if err := logFile.Close(); err != nil {
		t.Fatal(err)
	}

	recovered, err := repository.Open(dir)
	if err != nil {
		t.Fatalf("仓储应容忍崩溃留下的未完成尾部: %v", err)
	}
	loaded, err := recovered.Get(pkg.ID)
	if err != nil {
		recovered.Close()
		t.Fatal(err)
	}
	if err := loaded.AddSensitiveSite(domain.SensitiveSite{
		ID:                         "site-after-recovery",
		Category:                   "entrance",
		OriginalCoordinate:         domain.Coordinate{X: 10, Y: 20},
		ProtectionReason:           "保护入口",
		RecommendedPrecisionMeters: 50,
		RecordedBy:                 "surveyor",
	}, now.Add(time.Minute)); err != nil {
		recovered.Close()
		t.Fatal(err)
	}
	if err := recovered.Commit(pkg.ID, 1, "sensitive_site.recorded", loaded, "site-after-recovery", json.RawMessage(`{"version":2}`)); err != nil {
		recovered.Close()
		t.Fatalf("恢复后的提交应成功: %v", err)
	}
	if err := recovered.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := repository.Open(dir)
	if err != nil {
		t.Fatalf("成功提交后再次重启失败，恢复时未截断撕裂日志尾部: %v", err)
	}
	defer restarted.Close()
	final, err := restarted.Get(pkg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Version != 2 || len(final.SensitiveSites) != 1 {
		t.Fatalf("重启后提交结果丢失: version=%d sites=%d", final.Version, len(final.SensitiveSites))
	}
}
