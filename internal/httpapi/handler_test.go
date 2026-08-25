package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"karst-map-release/internal/application"
	"karst-map-release/internal/repository"
)

func TestCreateIsIdempotentAndSensitiveResponseIsFiltered(t *testing.T) {
	handler := NewHandler(application.NewService(repository.NewMemoryStore()))
	createBody := `{"caveName":"接口测试洞穴","surveyBounds":{"minX":0,"minY":0,"maxX":10,"maxY":10},"coordinateReferenceSystem":"LOCAL","layerSummaries":[{"name":"map","featureCount":1}],"owner":"owner","expectedVersion":0}`
	first := perform(t, handler, http.MethodPost, "/v1/survey-packages", "same-key", createBody)
	second := perform(t, handler, http.MethodPost, "/v1/survey-packages", "same-key", createBody)
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated || first.Body.String() != second.Body.String() {
		t.Fatalf("幂等响应不一致: %d %d", first.Code, second.Code)
	}
	var created application.MutationResult
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	siteBody := `{"category":"entrance","originalCoordinate":{"x":1.25,"y":2.5},"protectionReason":"保护入口","recommendedPrecisionMeters":500,"recordedBy":"owner","expectedVersion":1}`
	site := perform(t, handler, http.MethodPost, "/v1/survey-packages/"+created.Package.ID+"/sensitive-sites", "site-key", siteBody)
	if site.Code != http.StatusOK {
		t.Fatalf("登记失败: %s", site.Body.String())
	}
	if strings.Contains(site.Body.String(), "originalCoordinate") || strings.Contains(site.Body.String(), "1.25") {
		t.Fatalf("响应泄露原始坐标: %s", site.Body.String())
	}
	stale := perform(t, handler, http.MethodPost, "/v1/survey-packages/"+created.Package.ID+"/sensitive-sites", "stale-key", siteBody)
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), "version_conflict") {
		t.Fatalf("陈旧版本未返回稳定冲突: %s", stale.Body.String())
	}
}

func TestProtocolValidation(t *testing.T) {
	handler := NewHandler(application.NewService(repository.NewMemoryStore()))
	missingKey := perform(t, handler, http.MethodPost, "/v1/survey-packages", "", `{}`)
	if missingKey.Code != http.StatusBadRequest || !strings.Contains(missingKey.Body.String(), "idempotency_key_required") {
		t.Fatal("应要求幂等键")
	}
	unknown := perform(t, handler, http.MethodPost, "/v1/survey-packages", "key", `{"unknown":true}`)
	if unknown.Code != http.StatusBadRequest || !strings.Contains(unknown.Body.String(), "invalid_request") {
		t.Fatal("应拒绝未知 JSON 字段")
	}
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK || recorder.Header().Get("X-Correlation-ID") == "" || recorder.Header().Get("X-Request-Deadline-Ms") != "10000" {
		t.Fatal("健康检查或协议响应头无效")
	}
}

func perform(t *testing.T, handler http.Handler, method, path, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	response := recorder.Result()
	defer response.Body.Close()
	b, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	recorder.Body = bytes.NewBuffer(b)
	return recorder
}
