package failedcommitstateleak_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"karst-map-release/internal/domain"
	"karst-map-release/internal/repository"
)

func TestFailedSnapshotCommitDoesNotPublishState(t *testing.T) {
	dir := t.TempDir()
	store, err := repository.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	aggregate, err := domain.CreatePackage(domain.NewPackage{
		ID:                        "pkg-failed-commit",
		CaveName:                  "失败提交回归洞穴",
		SurveyBounds:              domain.Bounds{MinX: 0, MinY: 0, MaxX: 10, MaxY: 10},
		CoordinateReferenceSystem: "LOCAL",
		LayerSummaries:            []domain.LayerSummary{{Name: "survey", FeatureCount: 1}},
		Owner:                     "surveyor",
	}, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	commitErr := store.Commit(aggregate.ID, 0, "package.created", aggregate, "create:key", json.RawMessage(`{"package":{"id":"pkg-failed-commit"}}`))
	if commitErr == nil {
		t.Fatal("测试前提失效：投影目录失效后提交应返回错误")
	}
	if _, err := store.Get(aggregate.ID); err == nil {
		t.Fatalf("TestFailedSnapshotCommitDoesNotPublishState: Commit 返回错误后聚合仍对查询可见")
	}
	if _, ok := store.GetIdempotency("create:key"); ok {
		t.Fatalf("TestFailedSnapshotCommitDoesNotPublishState: Commit 返回错误后幂等结果仍对重试可见")
	}
}
