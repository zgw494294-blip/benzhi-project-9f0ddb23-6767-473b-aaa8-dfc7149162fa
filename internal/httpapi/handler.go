package httpapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"karst-map-release/internal/application"
	"karst-map-release/internal/domain"
)

const maxRequestBytes = 1 << 20

type Handler struct {
	service             *application.Service
	mux                 *http.ServeMux
	correlationSequence atomic.Uint64
}

func NewHandler(service *application.Service) *Handler {
	h := &Handler{service: service, mux: http.NewServeMux()}
	h.routes()
	return h
}

func (h *Handler) routes() {
	h.mux.HandleFunc("GET /healthz", h.Health)
	h.mux.HandleFunc("POST /v1/survey-packages", h.CreatePackage)
	h.mux.HandleFunc("GET /v1/survey-packages/{packageId}", h.GetPackage)
	h.mux.HandleFunc("PATCH /v1/survey-packages/{packageId}/metadata", h.ReviseMetadata)
	h.mux.HandleFunc("GET /v1/survey-packages/{packageId}/sensitive-sites", h.ListSensitiveSites)
	h.mux.HandleFunc("POST /v1/survey-packages/{packageId}/sensitive-sites", h.AddSensitiveSite)
	h.mux.HandleFunc("POST /v1/survey-packages/{packageId}/sensitive-sites/{siteId}/revisions", h.ReviseSensitiveSite)
	h.mux.HandleFunc("GET /v1/survey-packages/{packageId}/sensitive-sites/{siteId}/revisions", h.ListSensitiveSiteRevisions)
	h.mux.HandleFunc("GET /v1/survey-packages/{packageId}/redaction-revisions", h.ListRevisions)
	h.mux.HandleFunc("POST /v1/survey-packages/{packageId}/redaction-revisions", h.SubmitRevision)
	h.mux.HandleFunc("POST /v1/survey-packages/{packageId}/redaction-revisions/preview", h.PreviewRevision)
	h.mux.HandleFunc("GET /v1/survey-packages/{packageId}/checks", h.GetCheckReport)
	h.mux.HandleFunc("GET /v1/survey-packages/{packageId}/findings", h.ListFindings)
	h.mux.HandleFunc("POST /v1/survey-packages/{packageId}/review", h.CompleteReview)
	h.mux.HandleFunc("POST /v1/survey-packages/{packageId}/review/decisions", h.SaveReviewDecisions)
	h.mux.HandleFunc("POST /v1/survey-packages/{packageId}/freeze", h.Freeze)
	h.mux.HandleFunc("GET /v1/survey-packages/{packageId}/credential", h.GetCredential)
	h.mux.HandleFunc("POST /v1/release-credentials/verify", h.VerifyCredential)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	correlationID := strings.TrimSpace(r.Header.Get("X-Correlation-ID"))
	if correlationID == "" || len(correlationID) > 128 {
		correlationID = fmt.Sprintf("req-%d-%d", time.Now().UnixMilli(), h.correlationSequence.Add(1))
	}
	w.Header().Set("X-Correlation-ID", correlationID)
	w.Header().Set("X-Request-Deadline-Ms", "10000")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	defer func() {
		if recover() != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "服务内部错误", correlationID)
		}
	}()
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	view, err := h.service.Health()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, view)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *Handler) CreatePackage(w http.ResponseWriter, r *http.Request) {
	key, ok := requireIdempotency(w, r)
	if !ok {
		return
	}
	var cmd application.CreatePackageCommand
	if !decodeRequest(w, r, &cmd) {
		return
	}
	if cmd.ExpectedVersion < 0 {
		protocolError(w, r, "expectedVersion 不能为负")
		return
	}
	result, err := h.service.CreatePackage(cmd, key)
	if err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (h *Handler) GetPackage(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.GetPackage(r.PathValue("packageId"))
	if err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ReviseMetadata(w http.ResponseWriter, r *http.Request) {
	key, ok := requireIdempotency(w, r)
	if !ok {
		return
	}
	var cmd application.ReviseMetadataCommand
	if !decodeRequest(w, r, &cmd) {
		return
	}
	if !positiveVersion(w, r, cmd.ExpectedVersion) {
		return
	}
	result, err := h.service.ReviseMetadata(r.PathValue("packageId"), cmd, key)
	if err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListSensitiveSites(w http.ResponseWriter, r *http.Request) {
	sites, err := h.service.GetSensitiveSites(r.PathValue("packageId"))
	if err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sensitiveSites": sites})
}

func (h *Handler) ReviseSensitiveSite(w http.ResponseWriter, r *http.Request) {
	key, ok := requireIdempotency(w, r)
	if !ok {
		return
	}
	var cmd application.ReviseSensitiveSiteCommand
	if !decodeRequest(w, r, &cmd) {
		return
	}
	if !positiveVersion(w, r, cmd.ExpectedVersion) {
		return
	}
	result, err := h.service.ReviseSensitiveSite(r.PathValue("packageId"), r.PathValue("siteId"), cmd, key)
	if err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListSensitiveSiteRevisions(w http.ResponseWriter, r *http.Request) {
	history, err := h.service.GetSensitiveSiteHistory(r.PathValue("packageId"), r.PathValue("siteId"))
	if err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sensitiveSiteId": r.PathValue("siteId"), "revisions": history})
}

func (h *Handler) ListRevisions(w http.ResponseWriter, r *http.Request) {
	revisions, err := h.service.GetRevisions(r.PathValue("packageId"))
	if err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"redactionRevisions": revisions})
}

func (h *Handler) GetCheckReport(w http.ResponseWriter, r *http.Request) {
	report, err := h.service.GetCheckReport(r.PathValue("packageId"))
	if err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *Handler) AddSensitiveSite(w http.ResponseWriter, r *http.Request) {
	key, ok := requireIdempotency(w, r)
	if !ok {
		return
	}
	var cmd application.AddSensitiveSiteCommand
	if !decodeRequest(w, r, &cmd) {
		return
	}
	if cmd.ExpectedVersion <= 0 {
		protocolError(w, r, "expectedVersion 必须大于零")
		return
	}
	type outcome struct {
		result application.MutationResult
		err    error
	}
	completed := make(chan outcome, 1)
	go func() {
		result, err := h.service.AddSensitiveSiteCtx(r.Context(), r.PathValue("packageId"), cmd, key)
		completed <- outcome{result: result, err: err}
	}()
	select {
	case <-r.Context().Done():
		writeError(w, http.StatusServiceUnavailable, "request_canceled", "请求已取消，未提交任何变更", correlationID(w))
		<-completed
		return
	case finished := <-completed:
		if finished.err != nil {
			mapError(w, r, finished.err)
			return
		}
		writeJSON(w, http.StatusOK, finished.result)
	}
}

func (h *Handler) SubmitRevision(w http.ResponseWriter, r *http.Request) {
	key, ok := requireIdempotency(w, r)
	if !ok {
		return
	}
	var cmd application.SubmitRevisionCommand
	if !decodeRequest(w, r, &cmd) {
		return
	}
	if cmd.ExpectedVersion <= 0 {
		protocolError(w, r, "expectedVersion 必须大于零")
		return
	}
	result, err := h.service.SubmitRevision(r.PathValue("packageId"), cmd, key)
	if err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) PreviewRevision(w http.ResponseWriter, r *http.Request) {
	var cmd application.PreviewRevisionCommand
	if !decodeRequest(w, r, &cmd) {
		return
	}
	result, err := h.service.PreviewRevision(r.PathValue("packageId"), cmd)
	if err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) ListFindings(w http.ResponseWriter, r *http.Request) {
	findings, err := h.service.GetFindings(r.PathValue("packageId"))
	if err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"findings": findings})
}

func (h *Handler) CompleteReview(w http.ResponseWriter, r *http.Request) {
	key, ok := requireIdempotency(w, r)
	if !ok {
		return
	}
	var cmd application.CompleteReviewCommand
	if !decodeRequest(w, r, &cmd) {
		return
	}
	if cmd.ExpectedVersion <= 0 {
		protocolError(w, r, "expectedVersion 必须大于零")
		return
	}
	result, err := h.service.CompleteReview(r.PathValue("packageId"), cmd, key)
	if err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) SaveReviewDecisions(w http.ResponseWriter, r *http.Request) {
	key, ok := requireIdempotency(w, r)
	if !ok {
		return
	}
	var cmd application.SaveDecisionsCommand
	if !decodeRequest(w, r, &cmd) {
		return
	}
	if !positiveVersion(w, r, cmd.ExpectedVersion) {
		return
	}
	result, err := h.service.SaveReviewDecisions(r.PathValue("packageId"), cmd, key)
	if err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) Freeze(w http.ResponseWriter, r *http.Request) {
	key, ok := requireIdempotency(w, r)
	if !ok {
		return
	}
	var cmd application.FreezeCommand
	if !decodeRequest(w, r, &cmd) {
		return
	}
	if cmd.ExpectedVersion <= 0 {
		protocolError(w, r, "expectedVersion 必须大于零")
		return
	}
	result, err := h.service.Freeze(r.PathValue("packageId"), cmd, key)
	if err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) GetCredential(w http.ResponseWriter, r *http.Request) {
	credential, err := h.service.GetCredential(r.PathValue("packageId"))
	if err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, credential)
}

func (h *Handler) VerifyCredential(w http.ResponseWriter, r *http.Request) {
	var request application.VerifyCredentialCommand
	if !decodeRequest(w, r, &request) {
		return
	}
	if strings.TrimSpace(request.PackageID) == "" || strings.TrimSpace(request.VerificationHash) == "" {
		protocolError(w, r, "packageId 和 verificationHash 不能为空")
		return
	}
	if !sha256Text(request.VerificationHash) || (request.ContentDigest != "" && !sha256Text(request.ContentDigest)) || (request.PolicyDigest != "" && !sha256Text(request.PolicyDigest)) || (request.ManifestDigest != "" && !sha256Text(request.ManifestDigest)) {
		protocolError(w, r, "摘要与 verificationHash 必须为 64 位十六进制 SHA-256")
		return
	}
	result, err := h.service.DiagnoseCredential(request)
	if err != nil {
		mapError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func sha256Text(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func positiveVersion(w http.ResponseWriter, r *http.Request, version int64) bool {
	if version <= 0 {
		protocolError(w, r, "expectedVersion 必须大于零")
		return false
	}
	return true
}

func decodeRequest(w http.ResponseWriter, r *http.Request, destination any) bool {
	if media := r.Header.Get("Content-Type"); media != "" && !strings.HasPrefix(strings.ToLower(media), "application/json") {
		protocolError(w, r, "Content-Type 必须为 application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		if strings.Contains(err.Error(), "request body too large") {
			writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "请求体超过 1 MiB", correlationID(w))
			return false
		}
		protocolError(w, r, "JSON 请求体无效")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		protocolError(w, r, "请求体只能包含一个 JSON 对象")
		return false
	}
	return true
}

func requireIdempotency(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || len(key) > 128 {
		writeError(w, http.StatusBadRequest, "idempotency_key_required", "Idempotency-Key 必填且不得超过 128 字符", correlationID(w))
		return "", false
	}
	return key, true
}

func protocolError(w http.ResponseWriter, r *http.Request, message string) {
	writeError(w, http.StatusBadRequest, "invalid_request", message, correlationID(w))
}

func mapError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		writeError(w, http.StatusServiceUnavailable, "request_canceled", "请求已取消，未提交任何变更", correlationID(w))
		return
	}
	if errors.Is(err, application.ErrStorageIntegrity) {
		writeError(w, http.StatusServiceUnavailable, "storage_integrity_failed", "存储完整性检查失败", correlationID(w))
		return
	}
	if application.IsVersionConflict(err) {
		writeError(w, http.StatusConflict, "version_conflict", err.Error(), correlationID(w))
		return
	}
	var domainErr *domain.Error
	if errors.As(err, &domainErr) {
		status := http.StatusUnprocessableEntity
		switch domainErr.Code {
		case "not_found":
			status = http.StatusNotFound
		case "state_conflict":
			status = http.StatusConflict
		}
		writeError(w, status, domainErr.Code, domainErr.Message, correlationID(w))
		return
	}
	writeError(w, http.StatusInternalServerError, "internal_error", "服务内部错误", correlationID(w))
}

type errorBody struct {
	Error struct {
		Code          string `json:"code"`
		Message       string `json:"message"`
		CorrelationID string `json:"correlationId"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, message, correlationID string) {
	var body errorBody
	body.Error.Code, body.Error.Message, body.Error.CorrelationID = code, message, correlationID
	writeJSON(w, status, body)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func correlationID(w http.ResponseWriter) string { return w.Header().Get("X-Correlation-ID") }
