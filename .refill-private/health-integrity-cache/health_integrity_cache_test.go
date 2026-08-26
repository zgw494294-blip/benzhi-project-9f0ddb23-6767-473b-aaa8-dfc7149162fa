package health_integrity_cache_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"karst-map-release/internal/application"
	"karst-map-release/internal/domain"
	"karst-map-release/internal/httpapi"
	"karst-map-release/internal/repository"
)

func TestHealthCacheDoesNotMaskRuntimeLogCorruption(t *testing.T) {
	dataDir := t.TempDir()
	store, err := repository.Open(dataDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	service := application.NewService(store)
	_, err = service.CreatePackage(application.CreatePackageCommand{
		CaveName:                  "缓存完整性测试洞穴",
		SurveyBounds:              domain.Bounds{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100},
		CoordinateReferenceSystem: "LOCAL",
		LayerSummaries:            []domain.LayerSummary{{Name: "survey", FeatureCount: 1}},
		Owner:                     "surveyor",
	}, "create-health-cache-test")
	if err != nil {
		t.Fatalf("create package: %v", err)
	}

	handler := httpapi.NewHandler(service)
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first /healthz status = %d, want %d", first.Code, http.StatusOK)
	}

	logPath := filepath.Join(dataDir, "events.jsonl")
	if err := os.WriteFile(logPath, []byte("{runtime-corruption}\n"), 0o640); err != nil {
		t.Fatalf("corrupt event log: %v", err)
	}
	if _, err := store.Health(); err == nil {
		t.Fatal("repository health unexpectedly accepted the corrupted event log")
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if second.Code != http.StatusServiceUnavailable {
		t.Fatalf("second /healthz status = %d, want %d", second.Code, http.StatusServiceUnavailable)
	}
}
