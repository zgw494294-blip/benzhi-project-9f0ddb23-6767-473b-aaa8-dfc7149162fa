package querycache_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"karst-map-release/internal/application"
	"karst-map-release/internal/httpapi"
	"karst-map-release/internal/repository"
)

func TestCrossServiceQueryCacheRefreshesAfterSharedStoreCommit(t *testing.T) {
	store := repository.NewMemoryStore()
	reader := httpapi.NewHandler(application.NewService(store))
	writer := httpapi.NewHandler(application.NewService(store))

	created := request(t, reader, http.MethodPost, "/v1/survey-packages", "create-cache-case", `{"caveName":"缓存前洞穴","surveyBounds":{"minX":0,"minY":0,"maxX":100,"maxY":100},"coordinateReferenceSystem":"LOCAL","layerSummaries":[{"name":"survey","featureCount":1}],"owner":"owner","expectedVersion":0}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("建档失败: %s", created.Body.String())
	}
	var initial application.MutationResult
	if err := json.Unmarshal(created.Body.Bytes(), &initial); err != nil {
		t.Fatal(err)
	}

	revised := request(t, writer, http.MethodPatch, "/v1/survey-packages/"+initial.Package.ID+"/metadata", "revise-cache-case", `{"caveName":"缓存后洞穴","surveyBounds":{"minX":0,"minY":0,"maxX":100,"maxY":100},"coordinateReferenceSystem":"LOCAL","layerSummaries":[{"name":"survey","featureCount":2}],"owner":"owner","actor":"editor","revisionReason":"补充测绘要素","expectedVersion":1}`)
	if revised.Code != http.StatusOK {
		t.Fatalf("修订失败: %s", revised.Body.String())
	}

	observed := request(t, reader, http.MethodGet, "/v1/survey-packages/"+initial.Package.ID, "", "")
	if observed.Code != http.StatusOK {
		t.Fatalf("查询失败: %s", observed.Body.String())
	}
	var current application.PackageView
	if err := json.Unmarshal(observed.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if current.Version != 2 || current.CaveName != "缓存后洞穴" {
		t.Fatalf("共享 Store 已提交 version=2，但 reader 缓存返回 version=%d caveName=%q", current.Version, current.CaveName)
	}
}

func request(t *testing.T, handler http.Handler, method, path, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}
