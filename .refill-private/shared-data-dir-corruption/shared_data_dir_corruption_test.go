package shareddatadircorruption_test

import (
	"encoding/json"
	"testing"
	"time"

	"karst-map-release/internal/domain"
	"karst-map-release/internal/repository"
)

type commitRequest struct {
	store *repository.FileStore
	id    string
	key   string
	start <-chan struct{}
	done  chan<- error
}

func TestSharedDataDirectoryCannotCorruptEventSequence(t *testing.T) {
	dir := t.TempDir()
	first, err := repository.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := repository.Open(dir)
	if err != nil {
		return
	}
	defer second.Close()

	firstAggregate := newPackage(t, "pkg-first")
	secondAggregate := newPackage(t, "pkg-second")
	firstStart := make(chan struct{})
	secondStart := make(chan struct{})
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	go controlledCommit(commitRequest{store: first, id: firstAggregate.ID, key: "first:key", start: firstStart, done: firstDone}, firstAggregate)
	go controlledCommit(commitRequest{store: second, id: secondAggregate.ID, key: "second:key", start: secondStart, done: secondDone}, secondAggregate)

	close(firstStart)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	close(secondStart)
	if err := <-secondDone; err != nil {
		return
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}

	recovered, err := repository.Open(dir)
	if err != nil {
		t.Fatalf("TestSharedDataDirectoryCannotCorruptEventSequence: 双实例提交后事件链无法恢复: %v", err)
	}
	defer recovered.Close()
	if _, err := recovered.Get(firstAggregate.ID); err != nil {
		t.Fatalf("TestSharedDataDirectoryCannotCorruptEventSequence: 首个实例的提交丢失: %v", err)
	}
	if _, err := recovered.Get(secondAggregate.ID); err != nil {
		t.Fatalf("TestSharedDataDirectoryCannotCorruptEventSequence: 第二个实例的提交丢失: %v", err)
	}
}

func controlledCommit(request commitRequest, aggregate *domain.SurveyPackage) {
	<-request.start
	request.done <- request.store.Commit(request.id, 0, "package.created", aggregate, request.key, json.RawMessage(`{}`))
}

func newPackage(t *testing.T, id string) *domain.SurveyPackage {
	t.Helper()
	aggregate, err := domain.CreatePackage(domain.NewPackage{
		ID:                        id,
		CaveName:                  "双实例回归洞穴",
		SurveyBounds:              domain.Bounds{MinX: 0, MinY: 0, MaxX: 10, MaxY: 10},
		CoordinateReferenceSystem: "LOCAL",
		LayerSummaries:            []domain.LayerSummary{{Name: "survey", FeatureCount: 1}},
		Owner:                     "surveyor",
	}, time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	return aggregate
}
