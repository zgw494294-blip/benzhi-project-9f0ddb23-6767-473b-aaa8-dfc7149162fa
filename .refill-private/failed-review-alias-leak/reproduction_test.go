package failed_review_alias_leak_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"karst-map-release/internal/application"
	"karst-map-release/internal/domain"
	"karst-map-release/internal/httpapi"
	"karst-map-release/internal/repository"
)

func TestFailedReviewDoesNotLeakPartialDecision(t *testing.T) {
	storeDir := t.TempDir()
	store, err := repository.Open(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	aggregate := &domain.SurveyPackage{
		ID: "pkg-alias", CaveName: "别名泄漏洞穴", Owner: "surveyor",
		Status: domain.StatusPendingReview, Version: 1, CreatedAt: now, UpdatedAt: now,
		RedactionRevisions: []domain.RedactionRevision{{ID: "rev-1", PackageID: "pkg-alias", Sequence: 1}},
		Findings: []domain.ReviewFinding{
			{ID: "finding-1", PackageID: "pkg-alias", RevisionID: "rev-1", RuleCode: "COORDINATE_EXPOSURE", Status: domain.FindingOpen},
			{ID: "finding-2", PackageID: "pkg-alias", RevisionID: "rev-1", RuleCode: "REFERENCE_INTEGRITY", Status: domain.FindingOpen},
		},
	}
	if err := store.Commit(aggregate.ID, 0, "checks.completed", aggregate, "setup", json.RawMessage(`{"package":{"id":"pkg-alias"}}`)); err != nil {
		t.Fatal(err)
	}

	handler := httpapi.NewHandler(application.NewService(store))
	body := `{"revisionId":"rev-1","reviewer":"reviewer","action":"approve","note":"尝试通过","decisions":[{"findingId":"finding-1","status":"resolved","resolution":"已人工核验"}],"expectedVersion":1}`
	request := httptest.NewRequest(http.MethodPost, "/v1/survey-packages/pkg-alias/review", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "failed-review")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("预期未决发现阻止复核并返回 409，实际为 %d: %s", response.Code, response.Body.String())
	}

	query := httptest.NewRequest(http.MethodGet, "/v1/survey-packages/pkg-alias", nil)
	queryResponse := httptest.NewRecorder()
	handler.ServeHTTP(queryResponse, query)
	if queryResponse.Code != http.StatusOK {
		t.Fatalf("查询成果包失败: %d", queryResponse.Code)
	}
	var view application.PackageView
	if err := json.Unmarshal(queryResponse.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Version != 1 || len(view.Findings) != 2 {
		t.Fatalf("失败请求意外改变了聚合版本或发现数量: version=%d findings=%d", view.Version, len(view.Findings))
	}
	health, err := store.Health()
	if err != nil || health.EventSequence != 1 {
		t.Fatalf("失败请求后事件链异常: sequence=%d err=%v", health.EventSequence, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := repository.Open(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	persisted, err := reopened.Get("pkg-alias")
	if err != nil {
		t.Fatal(err)
	}
	if view.Findings[0].Status != domain.FindingOpen || persisted.Findings[0].Status != domain.FindingOpen {
		t.Fatalf("失败的复核请求污染了查询投影: 失败后=%s，重启后=%s，eventSequence=%d，期望均为 %s", view.Findings[0].Status, persisted.Findings[0].Status, health.EventSequence, domain.FindingOpen)
	}
}
