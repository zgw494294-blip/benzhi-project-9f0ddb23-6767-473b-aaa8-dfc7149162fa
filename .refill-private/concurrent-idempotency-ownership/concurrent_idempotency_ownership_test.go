package concurrent_idempotency_ownership_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"karst-map-release/internal/application"
	"karst-map-release/internal/httpapi"
	"karst-map-release/internal/repository"
)

type phasedLookupStore struct {
	repository.Store

	mu          sync.Mutex
	lookupCalls int
	firstPhase  chan struct{}
	secondPhase chan struct{}
}

func newPhasedLookupStore(store repository.Store) *phasedLookupStore {
	return &phasedLookupStore{
		Store:       store,
		firstPhase:  make(chan struct{}),
		secondPhase: make(chan struct{}),
	}
}

func (s *phasedLookupStore) GetIdempotency(key string) (json.RawMessage, bool) {
	s.mu.Lock()
	s.lookupCalls++
	call := s.lookupCalls
	if call == 2 {
		close(s.firstPhase)
	}
	if call == 4 {
		close(s.secondPhase)
	}
	s.mu.Unlock()

	if call <= 2 {
		<-s.firstPhase
		return nil, false
	}
	if call <= 4 {
		<-s.secondPhase
		return nil, false
	}
	return s.Store.GetIdempotency(key)
}

func TestConcurrentCreateClaimsIdempotencyKeyOnce(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) repository.Store
	}{
		{
			name: "memory",
			open: func(*testing.T) repository.Store {
				return repository.NewMemoryStore()
			},
		},
		{
			name: "file",
			open: func(t *testing.T) repository.Store {
				store, err := repository.Open(t.TempDir())
				if err != nil {
					t.Fatalf("打开 FileStore: %v", err)
				}
				return store
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := test.open(t)
			defer base.Close()
			coordinated := newPhasedLookupStore(base)
			handlers := []http.Handler{
				httpapi.NewHandler(application.NewService(coordinated)),
				httpapi.NewHandler(application.NewService(coordinated)),
			}

			const body = `{"caveName":"并发洞穴","surveyBounds":{"minX":0,"minY":0,"maxX":100,"maxY":100},"coordinateReferenceSystem":"LOCAL","layerSummaries":[{"name":"survey","featureCount":1}],"owner":"surveyor","expectedVersion":0}`
			responses := make([]*httptest.ResponseRecorder, len(handlers))
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i := range handlers {
				wg.Add(1)
				go func(index int) {
					defer wg.Done()
					<-start
					req := httptest.NewRequest(http.MethodPost, "/v1/survey-packages", strings.NewReader(body))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("Idempotency-Key", "shared-create-key")
					responses[index] = httptest.NewRecorder()
					handlers[index].ServeHTTP(responses[index], req)
				}(i)
			}
			close(start)
			wg.Wait()

			health, err := base.Health()
			if err != nil {
				t.Fatalf("读取仓储健康状态: %v", err)
			}
			if health.PackageCount != 1 {
				t.Fatalf("同一 Idempotency-Key 的并发建档持久化了 %d 个成果包；响应状态为 %d 和 %d", health.PackageCount, responses[0].Code, responses[1].Code)
			}
		})
	}
}
