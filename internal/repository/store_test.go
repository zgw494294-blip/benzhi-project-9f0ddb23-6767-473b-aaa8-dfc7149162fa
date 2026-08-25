package repository

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"karst-map-release/internal/domain"
)

func TestFileStoreRecoveryAndIdempotency(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := domain.CreatePackage(domain.NewPackage{ID: "pkg", CaveName: "恢复测试", SurveyBounds: domain.Bounds{MinX: 0, MinY: 0, MaxX: 10, MaxY: 10}, CoordinateReferenceSystem: "LOCAL", LayerSummaries: []domain.LayerSummary{{Name: "map", FeatureCount: 0}}, Owner: "owner"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	result := json.RawMessage(`{"package":{"id":"pkg"}}`)
	if err := store.Commit("pkg", 0, "package.created", p, "create:key", result); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "projection.json")); err != nil {
		t.Fatal(err)
	}
	recovered, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	loaded, err := recovered.Get("pkg")
	if err != nil || loaded.Version != 1 {
		t.Fatalf("重放失败: %#v, %v", loaded, err)
	}
	got, ok := recovered.GetIdempotency("create:key")
	if !ok || string(got) != string(result) {
		t.Fatalf("幂等结果未恢复: %s", got)
	}
}

func TestFileStoreRejectsBrokenHashChain(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := domain.CreatePackage(domain.NewPackage{ID: "pkg", CaveName: "校验测试", SurveyBounds: domain.Bounds{MinX: 0, MinY: 0, MaxX: 10, MaxY: 10}, CoordinateReferenceSystem: "LOCAL", LayerSummaries: []domain.LayerSummary{{Name: "map"}}, Owner: "owner"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Commit("pkg", 0, "package.created", p, "key", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "events.jsonl")
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 20 {
		t.Fatal("事件日志异常短")
	}
	b[len(b)/2] ^= 1
	if err := os.WriteFile(logPath, b, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("篡改事件日志后应拒绝启动")
	}
}

func TestFileStoreRejectsTruncatedLogAgainstSnapshot(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := domain.CreatePackage(domain.NewPackage{ID: "pkg", CaveName: "截断测试", SurveyBounds: domain.Bounds{MinX: 0, MinY: 0, MaxX: 10, MaxY: 10}, CoordinateReferenceSystem: "LOCAL", LayerSummaries: []domain.LayerSummary{{Name: "map"}}, Owner: "owner"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Commit("pkg", 0, "package.created", p, "key", json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(filepath.Join(dir, "events.jsonl"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("事件日志短于投影快照时应拒绝启动")
	}
}
