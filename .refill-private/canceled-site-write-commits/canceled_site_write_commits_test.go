package canceled_site_write_commits_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"karst-map-release/internal/application"
	"karst-map-release/internal/domain"
	"karst-map-release/internal/httpapi"
	"karst-map-release/internal/repository"
)

type blockedGetStore struct {
	repository.Store
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *blockedGetStore) Get(packageID string) (*domain.SurveyPackage, error) {
	s.once.Do(func() {
		close(s.started)
		<-s.release
	})
	return s.Store.Get(packageID)
}

type notifyingRecorder struct {
	*httptest.ResponseRecorder
	wroteHeader chan struct{}
	once        sync.Once
}

func (r *notifyingRecorder) WriteHeader(statusCode int) {
	r.ResponseRecorder.WriteHeader(statusCode)
	r.once.Do(func() { close(r.wroteHeader) })
}

func TestCanceledSensitiveSiteWriteDoesNotCommit(t *testing.T) {
	base := repository.NewMemoryStore()
	store := &blockedGetStore{
		Store:   base,
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	service := application.NewService(store)
	created, err := service.CreatePackage(application.CreatePackageCommand{
		CaveName:                  "取消测试洞穴",
		SurveyBounds:              domain.Bounds{MinX: 0, MinY: 0, MaxX: 100, MaxY: 100},
		CoordinateReferenceSystem: "LOCAL",
		LayerSummaries:            []domain.LayerSummary{{Name: "survey", FeatureCount: 1}},
		Owner:                     "surveyor",
	}, "create-before-cancel")
	if err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"category":"entrance","originalCoordinate":{"x":10,"y":20},"protectionReason":"保护入口","recommendedPrecisionMeters":50,"recordedBy":"surveyor","expectedVersion":1}`)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/v1/survey-packages/"+created.Package.ID+"/sensitive-sites", bytes.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "site-canceled")
	recorder := &notifyingRecorder{ResponseRecorder: httptest.NewRecorder(), wroteHeader: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		httpapi.NewHandler(service).ServeHTTP(recorder, req)
		close(done)
	}()

	<-store.started
	cancel()
	<-recorder.wroteHeader
	close(store.release)
	<-done

	persisted, err := base.Get(created.Package.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Version != created.Package.Version || len(persisted.SensitiveSites) != 0 {
		t.Fatalf("取消响应后仍提交了敏感点位：version=%d sites=%d", persisted.Version, len(persisted.SensitiveSites))
	}
}
