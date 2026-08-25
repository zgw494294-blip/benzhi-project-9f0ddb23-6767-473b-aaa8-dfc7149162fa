package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"karst-map-release/internal/application"
	"karst-map-release/internal/domain"
	"karst-map-release/internal/httpapi"
	"karst-map-release/internal/repository"
)

const defaultAddress = "127.0.0.1:19081"

type config struct {
	address   string
	dataDir   string
	selfcheck bool
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("服务退出", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	if cfg.selfcheck {
		return runSelfcheck(cfg.address)
	}
	store, err := repository.Open(cfg.dataDir)
	if err != nil {
		return fmt.Errorf("恢复持久化数据: %w", err)
	}
	defer store.Close()
	service := application.NewService(store)
	server := newHTTPServer(cfg.address, httpapi.NewHandler(service))
	listener, err := net.Listen("tcp", cfg.address)
	if err != nil {
		return fmt.Errorf("监听 %s: %w", cfg.address, err)
	}
	slog.Info("洞穴测绘成果公开治理服务已启动", "address", listener.Addr().String(), "dataDir", cfg.dataDir)
	errChannel := make(chan error, 1)
	go func() { errChannel <- server.Serve(listener) }()
	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case serveErr := <-errChannel:
		if !errors.Is(serveErr, http.ErrServerClosed) {
			return serveErr
		}
		return nil
	case <-signalContext.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("关闭 HTTP 服务: %w", err)
		}
		slog.Info("服务已安全关闭")
		return nil
	}
}

func parseConfig(args []string) (config, error) {
	defaultAddr := defaultAddress
	if port := strings.TrimSpace(os.Getenv("PORT")); port != "" {
		if _, err := strconv.ParseUint(port, 10, 16); err != nil || port == "0" {
			return config{}, fmt.Errorf("PORT 必须是 1-65535 的端口号")
		}
		defaultAddr = "127.0.0.1:" + port
	}
	set := flag.NewFlagSet("karst-map-release", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	address := set.String("addr", defaultAddr, "HTTP 监听地址")
	dataDir := set.String("data-dir", filepath.Join("data", "karst-map-release"), "事件日志与投影目录")
	selfcheck := set.Bool("selfcheck", false, "执行真实 HTTP 业务冒烟后退出")
	if err := set.Parse(args); err != nil {
		return config{}, err
	}
	if set.NArg() != 0 {
		return config{}, fmt.Errorf("不接受位置参数")
	}
	if err := validateAddress(*address); err != nil {
		return config{}, err
	}
	if strings.TrimSpace(*dataDir) == "" {
		return config{}, fmt.Errorf("data-dir 不能为空")
	}
	return config{address: *address, dataDir: *dataDir, selfcheck: *selfcheck}, nil
}

func validateAddress(address string) error {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("addr 必须为 host:port: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("addr 必须使用明确的回环 IP，禁止公开绑定")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("addr 端口必须在 1-65535 范围内")
	}
	return nil
}

func newHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 4 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 12 * time.Second, IdleTimeout: 45 * time.Second, MaxHeaderBytes: 32 * 1024}
}

func runSelfcheck(address string) error {
	tempDir, err := os.MkdirTemp("", "karst-map-release-selfcheck-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	store, err := repository.Open(tempDir)
	if err != nil {
		return err
	}
	defer store.Close()
	server := newHTTPServer(address, httpapi.NewHandler(application.NewService(store)))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("selfcheck 监听失败: %w", err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	baseURL := "http://" + listener.Addr().String()
	client := &http.Client{Timeout: 3 * time.Second}
	if err := executeSelfcheck(ctx, client, baseURL); err != nil {
		server.Close()
		return err
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if serveErr := <-serveDone; !errors.Is(serveErr, http.ErrServerClosed) {
		return serveErr
	}
	fmt.Println("selfcheck 通过：建档、敏感点位、脱敏、检查、复核、冻结、发证与验证链路完整")
	return nil
}

func executeSelfcheck(ctx context.Context, client *http.Client, baseURL string) error {
	if err := selfcheckHealth(ctx, client, baseURL); err != nil {
		return err
	}
	create := application.CreatePackageCommand{CaveName: "自检洞穴", SurveyBounds: domain.Bounds{MinX: 10000, MinY: 60000, MaxX: 20000, MaxY: 70000}, CoordinateReferenceSystem: "EPSG:4547", LayerSummaries: []domain.LayerSummary{{Name: "survey", FeatureCount: 1}}, Owner: "selfcheck-surveyor", ExpectedVersion: 0}
	var created application.MutationResult
	if err := selfcheckJSON(ctx, client, http.MethodPost, baseURL+"/v1/survey-packages", "selfcheck-create", create, http.StatusCreated, &created); err != nil {
		return fmt.Errorf("建档: %w", err)
	}
	packageID := created.Package.ID
	site := application.AddSensitiveSiteCommand{Category: "cave_entrance", OriginalCoordinate: domain.Coordinate{X: 12345, Y: 67890}, ProtectionReason: "脆弱入口保护", RecommendedPrecisionMeters: 500, RecordedBy: "selfcheck-surveyor", ExpectedVersion: created.Package.Version}
	var siteResult application.MutationResult
	if err := selfcheckJSON(ctx, client, http.MethodPost, baseURL+"/v1/survey-packages/"+packageID+"/sensitive-sites", "selfcheck-site", site, http.StatusOK, &siteResult); err != nil {
		return fmt.Errorf("登记点位: %w", err)
	}
	siteID := siteResult.Package.SensitiveSites[0].ID
	coord := &domain.Coordinate{X: 12345, Y: 67890}
	revision := application.SubmitRevisionCommand{BaseDigest: siteResult.Package.BaseDigest, Transformations: []domain.Transformation{{Type: domain.TransformRemoveCoordinate, SourceSiteID: siteID, LayerName: "survey"}}, PublicLayers: []domain.PublicLayer{{Name: "survey", Features: []domain.PublicFeature{{ID: "feature-1", Name: "入口", SourceSiteID: siteID, Coordinate: coord}}}}, SubmittedBy: "selfcheck-surveyor", ExpectedVersion: siteResult.Package.Version}
	var revisionResult application.MutationResult
	if err := selfcheckJSON(ctx, client, http.MethodPost, baseURL+"/v1/survey-packages/"+packageID+"/redaction-revisions", "selfcheck-revision", revision, http.StatusOK, &revisionResult); err != nil {
		return fmt.Errorf("提交修订: %w", err)
	}
	if len(revisionResult.Package.Findings) != 0 {
		return fmt.Errorf("安全脱敏候选版产生了意外发现")
	}
	revisionID := revisionResult.Package.RedactionRevisions[0].ID
	review := application.CompleteReviewCommand{RevisionID: revisionID, Reviewer: "selfcheck-reviewer", Action: "approve", Note: "自动检查无未决发现", ExpectedVersion: revisionResult.Package.Version}
	var reviewResult application.MutationResult
	if err := selfcheckJSON(ctx, client, http.MethodPost, baseURL+"/v1/survey-packages/"+packageID+"/review", "selfcheck-review", review, http.StatusOK, &reviewResult); err != nil {
		return fmt.Errorf("专业复核: %w", err)
	}
	freeze := application.FreezeCommand{IssuedBy: "selfcheck-release-manager", ExpectedVersion: reviewResult.Package.Version}
	var freezeResult application.FreezeResult
	if err := selfcheckJSON(ctx, client, http.MethodPost, baseURL+"/v1/survey-packages/"+packageID+"/freeze", "selfcheck-freeze", freeze, http.StatusOK, &freezeResult); err != nil {
		return fmt.Errorf("冻结发证: %w", err)
	}
	verify := map[string]string{"packageId": packageID, "verificationHash": freezeResult.Credential.VerificationHash}
	var verifyResult struct {
		Valid bool `json:"valid"`
	}
	if err := selfcheckJSON(ctx, client, http.MethodPost, baseURL+"/v1/release-credentials/verify", "", verify, http.StatusOK, &verifyResult); err != nil {
		return fmt.Errorf("凭据验证: %w", err)
	}
	if !verifyResult.Valid {
		return fmt.Errorf("凭据校验结果为无效")
	}
	return nil
}

func selfcheckHealth(ctx context.Context, client *http.Client, baseURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("健康检查状态码 %d", resp.StatusCode)
	}
	return nil
}

func selfcheckJSON(ctx context.Context, client *http.Client, method, url, idempotencyKey string, input any, expectedStatus int, output any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return err
	}
	if resp.StatusCode != expectedStatus {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	if err := json.Unmarshal(responseBody, output); err != nil {
		return fmt.Errorf("响应 JSON 无效: %w", err)
	}
	return nil
}
