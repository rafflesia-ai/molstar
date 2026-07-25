package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sacha-ichbiah/molstar/internal/job"
)

func TestServeHealth(t *testing.T) {
	a := app{stdout: io.Discard, stderr: io.Discard}
	cmd := a.serveCommand()
	handler := a.serveHandler(&serveFlags{}, cmd)

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["ok"] != true || body["service"] != "headlessmolstar" {
		t.Fatalf("unexpected health response: %#v", body)
	}
	if ready, ok := body["ready"].(map[string]any); !ok || ready["ok"] != true {
		t.Fatalf("unexpected health readiness: %#v", body["ready"])
	}
}

func TestServeReadyEndpoint(t *testing.T) {
	a := app{stdout: io.Discard, stderr: io.Discard}
	cmd := a.serveCommand()
	handler := a.serveHandler(&serveFlags{}, cmd)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("expected ready 200, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["ok"] != true || body["required"] != false {
		t.Fatalf("unexpected ready response: %#v", body)
	}

	notReady := a.serveHandler(&serveFlags{prewarm: true}, cmd)
	response = httptest.NewRecorder()
	notReady.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected not-ready 503, got %d: %s", response.Code, response.Body.String())
	}
}

func TestServeCapabilitiesAndRPC(t *testing.T) {
	a := app{stdout: io.Discard, stderr: io.Discard}
	cmd := a.serveCommand()
	handler := a.serveHandler(&serveFlags{noWorker: true, rendererCommand: "false", dryRun: true}, cmd)

	capabilities := httptest.NewRecorder()
	handler.ServeHTTP(capabilities, httptest.NewRequest(http.MethodGet, "/capabilities", nil))
	if capabilities.Code != http.StatusOK {
		t.Fatalf("expected capabilities 200, got %d: %s", capabilities.Code, capabilities.Body.String())
	}
	var capabilitiesBody map[string]any
	if err := json.Unmarshal(capabilities.Body.Bytes(), &capabilitiesBody); err != nil {
		t.Fatal(err)
	}
	if capabilitiesBody["ok"] != true || capabilitiesBody["runtime"] == nil || capabilitiesBody["renderer_workers"] == nil {
		t.Fatalf("unexpected capabilities body: %#v", capabilitiesBody)
	}
	if ready, ok := capabilitiesBody["ready"].(map[string]any); !ok || ready["ok"] != true {
		t.Fatalf("unexpected capabilities readiness: %#v", capabilitiesBody["ready"])
	}

	j := minimalServeJob(t, t.TempDir())
	data, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "explain",
		"params":  map[string]any{"job": j},
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewReader(data)))
	if response.Code != http.StatusOK {
		t.Fatalf("expected rpc 200, got %d: %s", response.Code, response.Body.String())
	}
	var rpcBody struct {
		JSONRPC string         `json:"jsonrpc"`
		Result  map[string]any `json:"result"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &rpcBody); err != nil {
		t.Fatal(err)
	}
	if rpcBody.JSONRPC != "2.0" || rpcBody.Result["ok"] != true {
		t.Fatalf("unexpected rpc body: %#v", rpcBody)
	}
}

func TestServeOpenAPIAndMetrics(t *testing.T) {
	stdout, stderr, err := runAppForTest(context.Background(), "serve", "--openapi")
	if err != nil {
		t.Fatalf("serve --openapi failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var openapi map[string]any
	if err := json.Unmarshal([]byte(stdout), &openapi); err != nil {
		t.Fatalf("openapi stdout is not JSON: %v\n%s", err, stdout)
	}
	if openapi["openapi"] != "3.1.0" || openapi["paths"] == nil {
		t.Fatalf("unexpected openapi report: %#v", openapi)
	}
	paths, _ := openapi["paths"].(map[string]any)
	if paths["/metrics/prometheus"] == nil {
		t.Fatalf("openapi missing prometheus metrics path: %#v", paths)
	}
	renderPath, _ := paths["/render"].(map[string]any)
	renderPost, _ := renderPath["post"].(map[string]any)
	if samples, _ := renderPost["x-codeSamples"].([]any); len(samples) < 3 {
		t.Fatalf("openapi render operation missing client code samples: %#v", renderPost)
	}
	components, _ := openapi["components"].(map[string]any)
	examples, _ := components["examples"].(map[string]any)
	if examples["RenderJob"] == nil || examples["JSONRPCMetricsRequest"] == nil || examples["PythonRender"] == nil {
		t.Fatalf("openapi missing generated examples: %#v", examples)
	}

	a := app{stdout: io.Discard, stderr: io.Discard}
	cmd := a.serveCommand()
	handler := a.serveHandler(&serveFlags{dryRun: true}, cmd)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/schema/openapi", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("expected openapi 200, got %d: %s", response.Code, response.Body.String())
	}

	dir := t.TempDir()
	data, err := json.Marshal(minimalServeJob(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	renderResponse := httptest.NewRecorder()
	handler.ServeHTTP(renderResponse, httptest.NewRequest(http.MethodPost, "/render", bytes.NewReader(data)))
	if renderResponse.Code != http.StatusOK {
		t.Fatalf("expected render 200, got %d: %s", renderResponse.Code, renderResponse.Body.String())
	}

	metricsResponse := httptest.NewRecorder()
	handler.ServeHTTP(metricsResponse, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if metricsResponse.Code != http.StatusOK {
		t.Fatalf("expected metrics 200, got %d: %s", metricsResponse.Code, metricsResponse.Body.String())
	}
	var metrics map[string]any
	if err := json.Unmarshal(metricsResponse.Body.Bytes(), &metrics); err != nil {
		t.Fatal(err)
	}
	if metrics["ok"] != true || metrics["submitted"] != float64(1) || metrics["succeeded"] != float64(1) {
		t.Fatalf("unexpected metrics: %#v", metrics)
	}

	prometheusResponse := httptest.NewRecorder()
	handler.ServeHTTP(prometheusResponse, httptest.NewRequest(http.MethodGet, "/metrics/prometheus", nil))
	if prometheusResponse.Code != http.StatusOK {
		t.Fatalf("expected prometheus metrics 200, got %d: %s", prometheusResponse.Code, prometheusResponse.Body.String())
	}
	prometheusBody := prometheusResponse.Body.String()
	if !strings.Contains(prometheusBody, "headlessmolstar_jobs_submitted_total 1") ||
		!strings.Contains(prometheusBody, "headlessmolstar_jobs_succeeded_total 1") ||
		!strings.Contains(prometheusBody, "headlessmolstar_queue_wait_ms_bucket") ||
		!strings.Contains(prometheusBody, "headlessmolstar_render_duration_ms_bucket") {
		t.Fatalf("unexpected prometheus metrics:\n%s", prometheusBody)
	}

	rpcBody, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "metrics"})
	if err != nil {
		t.Fatal(err)
	}
	rpcResponse := httptest.NewRecorder()
	handler.ServeHTTP(rpcResponse, httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewReader(rpcBody)))
	if rpcResponse.Code != http.StatusOK {
		t.Fatalf("expected rpc metrics 200, got %d: %s", rpcResponse.Code, rpcResponse.Body.String())
	}
	var rpc struct {
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal(rpcResponse.Body.Bytes(), &rpc); err != nil {
		t.Fatal(err)
	}
	if rpc.Result["submitted"] != float64(1) {
		t.Fatalf("unexpected rpc metrics: %#v", rpc)
	}
}

func TestRPCClientCommands(t *testing.T) {
	a := app{stdout: io.Discard, stderr: io.Discard}
	cmd := a.serveCommand()
	handler := a.serveHandler(&serveFlags{noWorker: true, rendererCommand: "false", dryRun: true}, cmd)
	server := httptest.NewServer(handler)
	defer server.Close()

	stdout, stderr, err := runAppForTest(context.Background(), "rpc", "capabilities", "--url", server.URL, "--json")
	if err != nil {
		t.Fatalf("rpc capabilities failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var capabilities struct {
		JSONRPC string         `json:"jsonrpc"`
		Result  map[string]any `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout), &capabilities); err != nil {
		t.Fatalf("rpc capabilities output is not JSON: %v\n%s", err, stdout)
	}
	if capabilities.JSONRPC != "2.0" || capabilities.Result["ok"] != true {
		t.Fatalf("unexpected rpc capabilities response: %#v", capabilities)
	}

	stdout, stderr, err = runAppForTest(context.Background(), "rpc", "metrics", "--url", server.URL, "--json")
	if err != nil {
		t.Fatalf("rpc metrics failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var metrics struct {
		JSONRPC string         `json:"jsonrpc"`
		Result  map[string]any `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout), &metrics); err != nil {
		t.Fatalf("rpc metrics output is not JSON: %v\n%s", err, stdout)
	}
	if metrics.JSONRPC != "2.0" || metrics.Result["ok"] != true {
		t.Fatalf("unexpected rpc metrics response: %#v", metrics)
	}

	dir := t.TempDir()
	j := minimalServeJob(t, dir)
	jobPath := filepath.Join(dir, "job.json")
	data, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jobPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = runAppForTest(context.Background(), "rpc", "explain", jobPath, "--url", server.URL, "--json")
	if err != nil {
		t.Fatalf("rpc explain failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var explain struct {
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout), &explain); err != nil {
		t.Fatalf("rpc explain output is not JSON: %v\n%s", err, stdout)
	}
	if explain.Result["ok"] != true || explain.Result["schema"] != "job-v1" {
		t.Fatalf("unexpected rpc explain response: %#v", explain)
	}
}

func TestServeUnixSocketHealth(t *testing.T) {
	socketPath := shortUnixSocketPath(t, "molstar.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	a := app{stdout: &stdout, stderr: &stderr}
	cmd := a.serveCommand()
	done := make(chan error, 1)
	go func() {
		done <- a.runServe(ctx, &serveFlags{socket: socketPath, noWorker: true}, cmd)
	}()
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("serve exited early: %v\nstdout:%s\nstderr:%s", err, stdout.String(), stderr.String())
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	client := http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return net.Dial("unix", socketPath)
	}}}
	response, err := client.Get("http://unix/health")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("expected unix health 200, got %d: %s", response.StatusCode, string(body))
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not shut down")
	}
}

func TestServeSmokeUnixSocket(t *testing.T) {
	socketPath := shortUnixSocketPath(t, "molstar.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	a := app{stdout: &stdout, stderr: &stderr}
	cmd := a.serveCommand()
	done := make(chan error, 1)
	go func() {
		done <- a.runServe(ctx, &serveFlags{socket: socketPath, noWorker: true, rendererCommand: "false"}, cmd)
	}()
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("serve exited early: %v\nstdout:%s\nstderr:%s", err, stdout.String(), stderr.String())
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	smokeStdout, smokeStderr, err := runAppForTest(context.Background(), "serve", "smoke", "--socket", socketPath, "--json")
	if err != nil {
		t.Fatalf("serve smoke failed: %v\nstdout:\n%s\nstderr:\n%s", err, smokeStdout, smokeStderr)
	}
	var report serveSmokeReport
	if err := json.Unmarshal([]byte(smokeStdout), &report); err != nil {
		t.Fatalf("serve smoke stdout is not JSON: %v\n%s", err, smokeStdout)
	}
	if !report.OK || len(report.Checks) < 5 {
		t.Fatalf("unexpected serve smoke report: %#v", report)
	}
	for _, check := range report.Checks {
		if !check.OK {
			t.Fatalf("serve smoke check failed: %#v", check)
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not shut down")
	}
}

func TestServeSmokeRenderProbeUnixSocket(t *testing.T) {
	socketPath := shortUnixSocketPath(t, "molstar.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	a := app{stdout: &stdout, stderr: &stderr}
	cmd := a.serveCommand()
	done := make(chan error, 1)
	go func() {
		done <- a.runServe(ctx, &serveFlags{socket: socketPath, noWorker: true}, cmd)
	}()
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(socketPath); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("serve exited early: %v\nstdout:%s\nstderr:%s", err, stdout.String(), stderr.String())
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	probeDir := t.TempDir()
	smokeStdout, smokeStderr, err := runAppForTest(context.Background(), "serve", "smoke", "--socket", socketPath, "--render-probe", "--probe-out-dir", probeDir, "--probe-timeout", "60s", "--json")
	if err != nil {
		t.Fatalf("serve smoke --render-probe failed: %v\nstdout:\n%s\nstderr:\n%s", err, smokeStdout, smokeStderr)
	}
	var report serveSmokeReport
	if err := json.Unmarshal([]byte(smokeStdout), &report); err != nil {
		t.Fatalf("serve smoke --render-probe stdout is not JSON: %v\n%s", err, smokeStdout)
	}
	var probe *serveSmokeCheck
	for i := range report.Checks {
		if report.Checks[i].Name == "render_probe" {
			probe = &report.Checks[i]
			break
		}
	}
	if !report.OK || probe == nil || !probe.OK || probe.JobID == "" || probe.Outputs == 0 || probe.DownloadedOutputs == 0 {
		t.Fatalf("unexpected render probe smoke report: %#v", report)
	}
	if err := requireNonEmptyFile(filepath.Join(probeDir, "downloads", "serve-smoke.png")); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not shut down")
	}
}

func TestServeAuthProtectsNonHealthEndpoints(t *testing.T) {
	a := app{stdout: io.Discard, stderr: io.Discard}
	cmd := a.serveCommand()
	handler := a.serveHandler(&serveFlags{authToken: "secret"}, cmd)

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("expected public health 200, got %d: %s", health.Code, health.Body.String())
	}
	ready := httptest.NewRecorder()
	handler.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if ready.Code != http.StatusOK {
		t.Fatalf("expected public ready 200, got %d: %s", ready.Code, ready.Body.String())
	}

	blocked := httptest.NewRecorder()
	handler.ServeHTTP(blocked, httptest.NewRequest(http.MethodGet, "/schema", nil))
	if blocked.Code != http.StatusUnauthorized {
		t.Fatalf("expected protected schema 401, got %d: %s", blocked.Code, blocked.Body.String())
	}

	allowedRequest := httptest.NewRequest(http.MethodGet, "/schema", nil)
	allowedRequest.Header.Set("Authorization", "Bearer secret")
	allowed := httptest.NewRecorder()
	handler.ServeHTTP(allowed, allowedRequest)
	if allowed.Code != http.StatusOK {
		t.Fatalf("expected authorized schema 200, got %d: %s", allowed.Code, allowed.Body.String())
	}
}

func TestServeForegroundReport(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var stdout string
	var stderr string
	var err error
	go func() {
		defer close(done)
		stdout, stderr, err = runAppForTest(ctx,
			"serve",
			"--addr", "127.0.0.1:0",
			"--foreground-report",
			"--workers", "2",
			"--queue", "3",
			"--dry-run",
		)
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done
	if err != nil && !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("serve failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "molstar serve\thttp://") || !strings.Contains(stdout, "workers\t2") || !strings.Contains(stdout, "queue\t3") || !strings.Contains(stdout, "health\thttp://") {
		t.Fatalf("unexpected foreground report:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestServerClientCommands(t *testing.T) {
	dir := t.TempDir()
	j := minimalServeJob(t, dir)
	jobPath := filepath.Join(dir, "job.json")
	data, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jobPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	a := app{stdout: io.Discard, stderr: io.Discard}
	cmd := a.serveCommand()
	handler := a.serveHandler(&serveFlags{dryRun: true, authToken: "secret"}, cmd)
	server := httptest.NewServer(handler)
	defer server.Close()

	stdout, stderr, err := runAppForTest(context.Background(), "server", "submit", jobPath, "--url", server.URL, "--token", "secret", "--json")
	if err != nil {
		t.Fatalf("server submit failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var submitted struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(stdout), &submitted); err != nil {
		t.Fatalf("submit stdout is not JSON: %v\n%s", err, stdout)
	}
	if submitted.ID == "" {
		t.Fatalf("expected submitted job id: %#v", submitted)
	}

	for i := 0; i < 50; i++ {
		stdout, stderr, err = runAppForTest(context.Background(), "server", "status", submitted.ID, "--url", server.URL, "--token", "secret", "--json")
		if err != nil {
			t.Fatalf("server status failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
		}
		var status struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal([]byte(stdout), &status); err != nil {
			t.Fatalf("status stdout is not JSON: %v\n%s", err, stdout)
		}
		if status.Status == "succeeded" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	stdout, stderr, err = runAppForTest(context.Background(), "server", "events", submitted.ID, "--url", server.URL, "--token", "secret", "--json")
	if err != nil {
		t.Fatalf("server events failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var events struct {
		OK     bool             `json:"ok"`
		Events []map[string]any `json:"events"`
	}
	if err := json.Unmarshal([]byte(stdout), &events); err != nil {
		t.Fatalf("events stdout is not JSON: %v\n%s", err, stdout)
	}
	if !events.OK || len(events.Events) == 0 {
		t.Fatalf("expected job events: %#v", events)
	}

	stdout, stderr, err = runAppForTest(context.Background(), "server", "cancel", submitted.ID, "--url", server.URL, "--token", "secret", "--json")
	if err != nil {
		t.Fatalf("server cancel failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("cancel stdout is not JSON:\n%s", stdout)
	}

	stdout, stderr, err = runAppForTest(context.Background(), "server", "status", submitted.ID, "--url", server.URL, "--json")
	if err == nil {
		t.Fatalf("expected server status without token to fail\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

func TestServerWaitLogsAndDownloadOutputs(t *testing.T) {
	dir := t.TempDir()
	j := minimalServeExportJob(t, dir)
	jobPath := filepath.Join(dir, "job.json")
	data, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jobPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	a := app{stdout: io.Discard, stderr: io.Discard}
	cmd := a.serveCommand()
	handler := a.serveHandler(&serveFlags{}, cmd)
	server := httptest.NewServer(handler)
	defer server.Close()

	stdout, stderr, err := runAppForTest(context.Background(), "server", "submit", jobPath, "--url", server.URL, "--json")
	if err != nil {
		t.Fatalf("server submit failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var submitted struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(stdout), &submitted); err != nil {
		t.Fatalf("submit stdout is not JSON: %v\n%s", err, stdout)
	}
	if submitted.ID == "" {
		t.Fatalf("expected submitted id: %#v", submitted)
	}

	downloadDir := filepath.Join(dir, "downloads")
	stdout, stderr, err = runAppForTest(context.Background(),
		"server", "wait", submitted.ID,
		"--url", server.URL,
		"--timeout", "5s",
		"--interval", "10ms",
		"--download-outputs",
		"--out-dir", downloadDir,
		"--json",
	)
	if err != nil {
		t.Fatalf("server wait failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var waited struct {
		Status            string                   `json:"status"`
		DownloadedOutputs []serverDownloadedOutput `json:"downloaded_outputs"`
	}
	if err := json.Unmarshal([]byte(stdout), &waited); err != nil {
		t.Fatalf("wait stdout is not JSON: %v\n%s", err, stdout)
	}
	if waited.Status != "succeeded" || len(waited.DownloadedOutputs) != 1 {
		t.Fatalf("unexpected wait report: %#v", waited)
	}
	if err := requireNonEmptyFile(filepath.Join(downloadDir, "scene.mvsj")); err != nil {
		t.Fatal(err)
	}

	submitWaitDir := filepath.Join(dir, "submit-wait-downloads")
	stdout, stderr, err = runAppForTest(context.Background(),
		"server", "submit", jobPath,
		"--url", server.URL,
		"--wait",
		"--wait-timeout", "5s",
		"--interval", "10ms",
		"--download-outputs",
		"--out-dir", submitWaitDir,
		"--json",
	)
	if err != nil {
		t.Fatalf("server submit --wait failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var submitWaited struct {
		OK        bool `json:"ok"`
		Submitted struct {
			ID string `json:"id"`
		} `json:"submitted"`
		Job struct {
			Status            string                   `json:"status"`
			DownloadedOutputs []serverDownloadedOutput `json:"downloaded_outputs"`
		} `json:"job"`
	}
	if err := json.Unmarshal([]byte(stdout), &submitWaited); err != nil {
		t.Fatalf("submit --wait stdout is not JSON: %v\n%s", err, stdout)
	}
	if !submitWaited.OK || submitWaited.Submitted.ID == "" || submitWaited.Job.Status != "succeeded" || len(submitWaited.Job.DownloadedOutputs) != 1 {
		t.Fatalf("unexpected submit --wait report: %#v", submitWaited)
	}
	if err := requireNonEmptyFile(filepath.Join(submitWaitDir, "scene.mvsj")); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err = runAppForTest(context.Background(), "server", "logs", submitted.ID, "--url", server.URL)
	if err != nil {
		t.Fatalf("server logs failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "events:") || !strings.Contains(stdout, "succeeded") || !strings.Contains(stdout, "scene.mvsj") {
		t.Fatalf("unexpected human logs:\n%s", stdout)
	}

	stdout, stderr, err = runAppForTest(context.Background(), "server", "logs", submitted.ID, "--url", server.URL, "--json")
	if err != nil {
		t.Fatalf("server logs --json failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("logs JSON output is not JSON:\n%s", stdout)
	}
}

func TestRenderGateCapsWorkersAndQueue(t *testing.T) {
	gate := newRenderGate(1, 0)
	if !gate.tryAcquire(context.Background()) {
		t.Fatal("first acquire failed")
	}
	if gate.tryAcquire(context.Background()) {
		t.Fatal("second acquire should be rejected when worker and queue capacity are full")
	}
	gate.release()
	if !gate.tryAcquire(context.Background()) {
		t.Fatal("acquire after release failed")
	}
	gate.release()
}

func TestServeRenderDryRun(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "model.cif")
	if err := os.WriteFile(modelPath, []byte("data_test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(dir, "render.png")
	j := job.Job{
		Version: 1,
		Inputs: map[string]job.Input{
			"input": {Path: modelPath, Format: "mmcif"},
		},
		Scene: job.Scene{
			Structures: []job.Structure{{
				Source: "input",
				Components: []job.Component{{
					Select: "all",
					Representation: job.Representation{
						Type:  "cartoon",
						Color: "element",
					},
				}},
			}},
		},
		Outputs: []job.Output{{
			Type: "image",
			Path: outputPath,
			Size: []int{64, 64},
		}},
	}
	data, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}

	a := app{stdout: io.Discard, stderr: io.Discard}
	cmd := a.serveCommand()
	handler := a.serveHandler(&serveFlags{dryRun: true}, cmd)
	request := httptest.NewRequest(http.MethodPost, "/render", bytes.NewReader(data))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var envelope struct {
		OK     bool         `json:"ok"`
		ID     string       `json:"id"`
		Status string       `json:"status"`
		Report renderReport `json:"report"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.ID == "" || envelope.Status != "succeeded" || !envelope.Report.OK {
		t.Fatalf("expected ok envelope: %#v", envelope)
	}
	if len(envelope.Report.Commands) != 1 || !envelope.Report.Commands[0].Skipped {
		t.Fatalf("expected one skipped render command, got %#v", envelope.Report.Commands)
	}
	if len(envelope.Report.OutputFiles) != 1 || envelope.Report.OutputFiles[0].Path != outputPath || envelope.Report.OutputFiles[0].Verified {
		t.Fatalf("unexpected dry-run output report: %#v", envelope.Report.OutputFiles)
	}
}

func TestServeRenderAsyncJobStatusAndEvents(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "model.cif")
	if err := os.WriteFile(modelPath, []byte("data_test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(dir, "render.png")
	j := job.Job{
		Version: 1,
		Inputs: map[string]job.Input{
			"input": {Path: modelPath, Format: "mmcif"},
		},
		Scene: job.Scene{Structures: []job.Structure{{
			Source: "input",
			Components: []job.Component{{
				Select:         "all",
				Representation: job.Representation{Type: "cartoon", Color: "element"},
			}},
		}}},
		Outputs: []job.Output{{Type: "image", Path: outputPath, Size: []int{64, 64}}},
	}
	data, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	a := app{stdout: io.Discard, stderr: io.Discard}
	cmd := a.serveCommand()
	handler := a.serveHandler(&serveFlags{dryRun: true}, cmd)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/render?async=true", bytes.NewReader(data)))
	if response.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", response.Code, response.Body.String())
	}
	var submitted struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &submitted); err != nil {
		t.Fatal(err)
	}
	if submitted.ID == "" || submitted.Status == "" {
		t.Fatalf("unexpected async response: %#v", submitted)
	}

	var statusBody map[string]any
	for i := 0; i < 50; i++ {
		status := httptest.NewRecorder()
		handler.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/jobs/"+submitted.ID, nil))
		if status.Code != http.StatusOK {
			t.Fatalf("expected job status 200, got %d: %s", status.Code, status.Body.String())
		}
		if err := json.Unmarshal(status.Body.Bytes(), &statusBody); err != nil {
			t.Fatal(err)
		}
		if statusBody["status"] == "succeeded" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if statusBody["status"] != "succeeded" {
		t.Fatalf("job did not succeed: %#v", statusBody)
	}

	events := httptest.NewRecorder()
	handler.ServeHTTP(events, httptest.NewRequest(http.MethodGet, "/jobs/"+submitted.ID+"/events", nil))
	if events.Code != http.StatusOK {
		t.Fatalf("expected events 200, got %d: %s", events.Code, events.Body.String())
	}
	if !bytes.Contains(events.Body.Bytes(), []byte(`"phase"`)) {
		t.Fatalf("expected jsonl events, got %q", events.Body.String())
	}
}

func TestServeLoadQueuePressureAndCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell sleep renderer smoke is unix-only")
	}
	dir := t.TempDir()
	rendererPath := filepath.Join(dir, "slow-renderer.sh")
	if err := os.WriteFile(rendererPath, []byte("#!/usr/bin/env bash\nsleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	j := minimalServeJob(t, dir)
	data, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	a := app{stdout: io.Discard, stderr: io.Discard}
	cmd := a.serveCommand()
	handler := a.serveHandler(&serveFlags{workers: 1, queue: 1, noWorker: true, rendererCommand: rendererPath}, cmd)

	submit := func(t *testing.T) (int, map[string]any) {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/render?async=true", bytes.NewReader(data)))
		var body map[string]any
		if response.Body.Len() > 0 {
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("response is not JSON: %v\n%s", err, response.Body.String())
			}
		}
		return response.Code, body
	}

	code, first := submit(t)
	if code != http.StatusAccepted {
		t.Fatalf("expected first submit 202, got %d: %#v", code, first)
	}
	code, second := submit(t)
	if code != http.StatusAccepted {
		t.Fatalf("expected queued submit 202, got %d: %#v", code, second)
	}
	code, rejected := submit(t)
	if code != http.StatusTooManyRequests {
		t.Fatalf("expected third submit 429, got %d: %#v", code, rejected)
	}
	if errBody, ok := rejected["error"].(map[string]any); !ok || errBody["code"] != string(kindServerBusy) || errBody["agent_code"] != string(kindServerBusy) {
		t.Fatalf("unexpected rejection error body: %#v", rejected)
	}

	secondID, _ := second["id"].(string)
	if secondID == "" {
		t.Fatalf("queued job missing id: %#v", second)
	}
	cancelResponse := httptest.NewRecorder()
	handler.ServeHTTP(cancelResponse, httptest.NewRequest(http.MethodDelete, "/jobs/"+secondID, nil))
	if cancelResponse.Code != http.StatusAccepted {
		t.Fatalf("expected cancel 202, got %d: %s", cancelResponse.Code, cancelResponse.Body.String())
	}
	var canceled map[string]any
	if err := json.Unmarshal(cancelResponse.Body.Bytes(), &canceled); err != nil {
		t.Fatal(err)
	}
	if canceled["status"] != "canceled" {
		t.Fatalf("unexpected canceled job status: %#v", canceled)
	}

	firstID, _ := first["id"].(string)
	if firstID != "" {
		cleanupResponse := httptest.NewRecorder()
		handler.ServeHTTP(cleanupResponse, httptest.NewRequest(http.MethodDelete, "/jobs/"+firstID, nil))
		waitForServeJobTerminal(t, handler, firstID, 6*time.Second)
	}
	waitForServeJobTerminal(t, handler, secondID, 2*time.Second)
}

func TestServeConcurrentQueueMetrics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell renderer smoke is unix-only")
	}
	dir := t.TempDir()
	rendererPath := writeSlowPNGRenderer(t, dir, "1")
	j := minimalServeJob(t, dir)
	data, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	a := app{stdout: io.Discard, stderr: io.Discard}
	cmd := a.serveCommand()
	handler := a.serveHandler(&serveFlags{workers: 2, queue: 2, noWorker: true, rendererCommand: rendererPath}, cmd)

	var ids []string
	for i := 0; i < 4; i++ {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/render?async=true", bytes.NewReader(data)))
		if response.Code != http.StatusAccepted {
			t.Fatalf("submit %d expected 202, got %d: %s", i, response.Code, response.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		id, _ := body["id"].(string)
		if id == "" {
			t.Fatalf("submitted job missing id: %#v", body)
		}
		ids = append(ids, id)
	}

	waitForServeMetrics(t, handler, 2*time.Second, func(metrics map[string]any) bool {
		return anyMetricFloat(metrics["active_jobs"]) == 2 && anyMetricFloat(metrics["queued_jobs"]) == 2
	})
	for _, id := range ids {
		waitForServeJobTerminal(t, handler, id, 5*time.Second)
	}
	metrics := serveMetricsForTest(t, handler)
	if anyMetricFloat(metrics["submitted"]) != 4 || anyMetricFloat(metrics["succeeded"]) != 4 || anyMetricFloat(metrics["rejected"]) != 0 {
		t.Fatalf("unexpected concurrency metrics: %#v", metrics)
	}
	renderDuration, _ := metrics["render_duration_ms"].(map[string]any)
	if anyMetricFloat(renderDuration["count"]) != 4 {
		t.Fatalf("expected four render duration samples: %#v", metrics)
	}
}

func TestServeRequestTimeoutCancelsRunningJob(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell renderer smoke is unix-only")
	}
	dir := t.TempDir()
	rendererPath := writeSlowPNGRenderer(t, dir, "5")
	j := minimalServeJob(t, dir)
	data, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	a := app{stdout: io.Discard, stderr: io.Discard}
	cmd := a.serveCommand()
	handler := a.serveHandler(&serveFlags{workers: 1, queue: 0, noWorker: true, rendererCommand: rendererPath, requestTimeoutSeconds: 1}, cmd)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/render", bytes.NewReader(data)))
	if response.Code != 499 {
		t.Fatalf("expected request timeout/cancel 499, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if errBody, ok := body["error"].(map[string]any); !ok || errBody["code"] != string(kindCanceled) {
		t.Fatalf("unexpected timeout error body: %#v", body)
	}
	waitForServeMetrics(t, handler, 3*time.Second, func(metrics map[string]any) bool {
		return anyMetricFloat(metrics["canceled"]) == 1
	})
}

func TestServeWorkerRestartAfterWorkerCrash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("python worker smoke is unix-only")
	}
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is required for worker restart smoke")
	}
	dir := t.TempDir()
	workerPath := writeFlakyWorkerRenderer(t, dir)
	j := minimalServeJob(t, dir)
	data, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	a := app{stdout: io.Discard, stderr: io.Discard}
	cmd := a.serveCommand()
	handler := a.serveHandler(&serveFlags{workers: 1, queue: 1, workerCommand: python + " " + workerPath}, cmd)

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/render", bytes.NewReader(data)))
	if first.Code != http.StatusBadGateway {
		t.Fatalf("expected first worker crash to return 502, got %d: %s", first.Code, first.Body.String())
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/render", bytes.NewReader(data)))
	if second.Code != http.StatusOK {
		t.Fatalf("expected replacement worker render 200, got %d: %s", second.Code, second.Body.String())
	}
	metrics := serveMetricsForTest(t, handler)
	workerPool, _ := metrics["worker_pool"].(map[string]any)
	if anyMetricFloat(workerPool["restarts"]) < 1 || anyMetricFloat(metrics["failed"]) != 1 || anyMetricFloat(metrics["succeeded"]) != 1 {
		t.Fatalf("unexpected worker restart metrics: %#v", metrics)
	}
}

func waitForServeJobTerminal(t *testing.T, handler http.Handler, id string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		statusResponse := httptest.NewRecorder()
		handler.ServeHTTP(statusResponse, httptest.NewRequest(http.MethodGet, "/jobs/"+id, nil))
		var status map[string]any
		if err := json.Unmarshal(statusResponse.Body.Bytes(), &status); err == nil {
			switch status["status"] {
			case "canceled", "failed", "succeeded":
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach terminal state", id)
}

func waitForServeMetrics(t *testing.T, handler http.Handler, timeout time.Duration, ready func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var metrics map[string]any
	for time.Now().Before(deadline) {
		metrics = serveMetricsForTest(t, handler)
		if ready(metrics) {
			return metrics
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("metrics condition did not become true: %#v", metrics)
	return nil
}

func serveMetricsForTest(t *testing.T, handler http.Handler) map[string]any {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("expected metrics 200, got %d: %s", response.Code, response.Body.String())
	}
	var metrics map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &metrics); err != nil {
		t.Fatal(err)
	}
	return metrics
}

func writeSlowPNGRenderer(t *testing.T, dir string, sleepSeconds string) string {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required for slow renderer smoke")
	}
	path := filepath.Join(dir, "slow-png-renderer.sh")
	script := `#!/usr/bin/env python3
import argparse
import pathlib
import struct
import sys
import time
import zlib

def write_png(path, width, height):
    def chunk(kind, data):
        return (
            struct.pack(">I", len(data))
            + kind
            + data
            + struct.pack(">I", zlib.crc32(kind + data) & 0xFFFFFFFF)
        )
    rows = []
    for y in range(height):
        row = bytearray([0])
        for x in range(width):
            red = 32 + (x * 160 // max(1, width - 1))
            green = 48 + (y * 160 // max(1, height - 1))
            row.extend((red, green, 160))
        rows.append(bytes(row))
    data = b"".join(rows)
    png = (
        b"\x89PNG\r\n\x1a\n"
        + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0))
        + chunk(b"IDAT", zlib.compress(data))
        + chunk(b"IEND", b"")
    )
    pathlib.Path(path).write_bytes(png)

parser = argparse.ArgumentParser(add_help=False)
parser.add_argument("-i", "--input")
parser.add_argument("-o", "--output")
parser.add_argument("--size")
parser.add_argument("--quiet", action="store_true")
args, _ = parser.parse_known_args()
if not args.output:
    print("missing output path", file=sys.stderr)
    raise SystemExit(2)
width, height = 64, 64
if args.size and "x" in args.size:
    raw_width, raw_height = args.size.lower().split("x", 1)
    width, height = int(raw_width), int(raw_height)
time.sleep(` + sleepSeconds + `)
pathlib.Path(args.output).parent.mkdir(parents=True, exist_ok=True)
write_png(args.output, width, height)
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeFlakyWorkerRenderer(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "flaky-worker.py")
	marker := filepath.Join(dir, "worker-crashed-once")
	script := `#!/usr/bin/env python3
import json
import os
import pathlib
import struct
import sys
import zlib

marker = ` + strconv.Quote(marker) + `

def png_bytes(width, height):
    def chunk(kind, data):
        return (
            struct.pack(">I", len(data))
            + kind
            + data
            + struct.pack(">I", zlib.crc32(kind + data) & 0xFFFFFFFF)
        )
    rows = []
    for y in range(height):
        row = bytearray([0])
        for x in range(width):
            row.extend((180, 40 + (x * 160 // max(1, width - 1)), 60 + (y * 160 // max(1, height - 1))))
        rows.append(bytes(row))
    return (
        b"\x89PNG\r\n\x1a\n"
        + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0))
        + chunk(b"IDAT", zlib.compress(b"".join(rows)))
        + chunk(b"IEND", b"")
    )

def write_response(payload):
    sys.stdout.write(json.dumps(payload) + "\n")
    sys.stdout.flush()

for line in sys.stdin:
    request = json.loads(line)
    request_id = request.get("id")
    method = request.get("method")
    if method == "shutdown":
        write_response({"id": request_id, "ok": True})
        break
    if method == "capabilities":
        write_response({"id": request_id, "ok": True, "result": {"ok": True, "gl": {"available": True}, "canvas": {"available": True}}})
        continue
    if method == "render":
        if not os.path.exists(marker):
            pathlib.Path(marker).write_text("crashed\n")
            sys.exit(1)
        params = request.get("params") or {}
        output = params.get("output")
        width = int(params.get("width") or 64)
        height = int(params.get("height") or 64)
        pathlib.Path(output).parent.mkdir(parents=True, exist_ok=True)
        pathlib.Path(output).write_bytes(png_bytes(width, height))
        write_response({"id": request_id, "ok": True, "result": {"input": params.get("input"), "output": output, "size": {"width": params.get("width"), "height": params.get("height")}}})
        continue
    write_response({"id": request_id, "ok": False, "error": {"message": "unsupported method"}})
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestJobsPruneCommand(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "jobs")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	records := []serverJobRecord{
		{ID: "old", Status: "succeeded", SubmittedAt: now.Add(-72 * time.Hour).Format(time.RFC3339Nano), FinishedAt: now.Add(-48 * time.Hour).Format(time.RFC3339Nano)},
		{ID: "new", Status: "succeeded", SubmittedAt: now.Add(-1 * time.Hour).Format(time.RFC3339Nano), FinishedAt: now.Format(time.RFC3339Nano)},
	}
	for _, record := range records {
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(store, record.ID+".json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	stdout, stderr, err := runAppForTest(context.Background(), "jobs", "prune", "--job-store", store, "--ttl", "24h", "--dry-run", "--json")
	if err != nil {
		t.Fatalf("jobs prune dry-run failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(store, "old.json")); err != nil {
		t.Fatalf("dry-run removed old record: %v", err)
	}
	var dryRunReport struct {
		OK      bool              `json:"ok"`
		DryRun  bool              `json:"dry_run"`
		Removed []prunedJobRecord `json:"removed"`
	}
	if err := json.Unmarshal([]byte(stdout), &dryRunReport); err != nil {
		t.Fatalf("jobs prune dry-run output is not JSON: %v\n%s", err, stdout)
	}
	if !dryRunReport.OK || !dryRunReport.DryRun || len(dryRunReport.Removed) != 1 || dryRunReport.Removed[0].ID != "old" {
		t.Fatalf("unexpected dry-run report: %#v", dryRunReport)
	}

	stdout, stderr, err = runAppForTest(context.Background(), "jobs", "prune", "--job-store", store, "--ttl", "24h", "--json")
	if err != nil {
		t.Fatalf("jobs prune failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(store, "old.json")); !os.IsNotExist(err) {
		t.Fatalf("expected old record to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(store, "new.json")); err != nil {
		t.Fatalf("expected new record to remain: %v", err)
	}
}

func TestRPCErrorCodeMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"unknown method", markError(kindInvalidInput, fmt.Errorf("%w %q", errRPCMethodNotFound, "nope")), -32601},
		{"invalid input", markError(kindInvalidInput, errors.New("version is required")), -32602},
		{"validation", markError(kindValidation, errors.New("bad")), -32602},
		{"canceled", markError(kindCanceled, errors.New("ctx canceled")), -32800},
		{"internal", markError(kindRender, errors.New("boom")), -32000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rpcErrorCode(tc.err); got != tc.want {
				t.Fatalf("rpcErrorCode = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestPruneJobStoreIgnoresAppleDoubleFiles(t *testing.T) {
	store := t.TempDir()
	now := time.Now().UTC()
	record := serverJobRecord{ID: "old", Status: "succeeded", SubmittedAt: now.Add(-72 * time.Hour).Format(time.RFC3339Nano), FinishedAt: now.Add(-48 * time.Hour).Format(time.RFC3339Nano)}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "old.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	// A macOS AppleDouble sidecar (binary, not valid JSON) must not abort prune.
	if err := os.WriteFile(filepath.Join(store, "._old.json"), []byte{0x00, 0x05, 0x16, 0x07}, 0o644); err != nil {
		t.Fatal(err)
	}

	removed, err := pruneJobStore(store, 24*time.Hour, now, false)
	if err != nil {
		t.Fatalf("prune aborted on an AppleDouble file: %v", err)
	}
	if len(removed) != 1 || removed[0].ID != "old" {
		t.Fatalf("unexpected prune result: %#v", removed)
	}
}

func TestServeJobStorePersistsAndReloadsJob(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "model.cif")
	if err := os.WriteFile(modelPath, []byte("data_test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(dir, "render.png")
	j := job.Job{
		Version: 1,
		Inputs: map[string]job.Input{
			"input": {Path: modelPath, Format: "mmcif"},
		},
		Scene: job.Scene{Structures: []job.Structure{{
			Source: "input",
			Components: []job.Component{{
				Select:         "all",
				Representation: job.Representation{Type: "cartoon", Color: "element"},
			}},
		}}},
		Outputs: []job.Output{{Type: "image", Path: outputPath, Size: []int{64, 64}}},
	}
	data, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	jobStore := filepath.Join(dir, "jobs")
	a := app{stdout: io.Discard, stderr: io.Discard}
	cmd := a.serveCommand()
	handler := a.serveHandler(&serveFlags{dryRun: true, jobStore: jobStore}, cmd)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/render", bytes.NewReader(data)))
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(jobStore, "job_1.json")); err != nil {
		t.Fatal(err)
	}

	reloaded := a.serveHandler(&serveFlags{dryRun: true, jobStore: jobStore}, cmd)
	status := httptest.NewRecorder()
	reloaded.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/jobs/job_1", nil))
	if status.Code != http.StatusOK {
		t.Fatalf("expected reloaded status 200, got %d: %s", status.Code, status.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(status.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "succeeded" {
		t.Fatalf("unexpected reloaded job: %#v", body)
	}
}

func minimalServeJob(t *testing.T, dir string) job.Job {
	t.Helper()
	modelPath := filepath.Join(dir, "model.cif")
	if err := os.WriteFile(modelPath, []byte("data_test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return job.Job{
		Version: 1,
		Inputs: map[string]job.Input{
			"input": {Path: modelPath, Format: "mmcif"},
		},
		Scene: job.Scene{Structures: []job.Structure{{
			Source: "input",
			Components: []job.Component{{
				Select:         "all",
				Representation: job.Representation{Type: "cartoon", Color: "element"},
			}},
		}}},
		Outputs: []job.Output{{Type: "image", Path: filepath.Join(dir, "render.png"), Size: []int{64, 64}}},
	}
}

func minimalServeExportJob(t *testing.T, dir string) job.Job {
	t.Helper()
	modelPath := filepath.Join(dir, "model.cif")
	if err := os.WriteFile(modelPath, []byte(oneAtomCIF), 0o644); err != nil {
		t.Fatal(err)
	}
	return job.Job{
		Version: 1,
		Inputs: map[string]job.Input{
			"input": {Path: modelPath, Format: "mmcif"},
		},
		Scene: job.Scene{Structures: []job.Structure{{
			Source: "input",
			Components: []job.Component{{
				Select:         "all",
				Representation: job.Representation{Type: "spacefill", Color: "#cc3399"},
			}},
		}}},
		Outputs: []job.Output{{Type: "mvsj", Path: filepath.Join(dir, "scene.mvsj")}},
	}
}

// TestRPCRenderAppliesRuntimePolicy proves a job submitted via /rpc render is
// subject to the operator's serve-time hardening (here --allow-path), matching
// REST /render. Without applyRuntimeFlags in the RPC render branch, the policy
// would be silently bypassed.
func TestRPCRenderAppliesRuntimePolicy(t *testing.T) {
	a := app{stdout: io.Discard, stderr: io.Discard}
	cmd := a.serveCommand()
	allowedDir := t.TempDir()
	if err := cmd.Flags().Set("allow-path", allowedDir); err != nil {
		t.Fatal(err)
	}
	sf := &serveFlags{noWorker: true, rendererCommand: "false", dryRun: true}
	sf.runtime.allowPaths = []string{allowedDir}
	handler := a.serveHandler(sf, cmd)

	// The job lives entirely outside the operator's allowed root.
	j := minimalServeJob(t, t.TempDir())
	data, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "render",
		"params":  map[string]any{"job": j},
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/rpc", bytes.NewReader(data)))
	if !strings.Contains(response.Body.String(), "outside allowed roots") {
		t.Fatalf("expected /rpc render to enforce operator allow-path policy, got: %s", response.Body.String())
	}
}
