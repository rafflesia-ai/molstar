package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/molstar/internal/job"
	"github.com/rafflesia-ai/molstar/internal/mvs"
	"github.com/rafflesia-ai/molstar/internal/render"
)

const maxHTTPJobBytes int64 = 32 << 20

type serveFlags struct {
	addr                  string
	socket                string
	workers               int
	queue                 int
	requestTimeoutSeconds int
	rendererCommand       string
	workerCommand         string
	jobStore              string
	jobTTL                string
	authToken             string
	openapi               bool
	prewarm               bool
	foregroundReport      bool
	noWorker              bool
	dryRun                bool
	quiet                 bool
	verbose               bool
	runtime               runtimeFlags
}

func (a app) serveCommand() *cobra.Command {
	flags := &serveFlags{}
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run a local HTTP headless Mol* job server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("serve", true, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("serve does not accept positional arguments"))
				}
				if flags.openapi {
					return writeJSON(a.stdout, serveOpenAPISchema())
				}
				if flags.quiet && flags.verbose {
					return markError(kindInvalidInput, fmt.Errorf("--quiet and --verbose cannot be used together"))
				}
				return a.runServe(cmd.Context(), flags, cmd)
			})
		},
	}
	cmd.Flags().StringVar(&flags.addr, "addr", "127.0.0.1:8080", "listen address")
	cmd.Flags().StringVar(&flags.socket, "socket", "", "listen on a Unix socket instead of TCP")
	cmd.Flags().IntVar(&flags.workers, "workers", 1, "maximum concurrent render jobs")
	cmd.Flags().IntVar(&flags.queue, "queue", 16, "maximum queued render jobs before returning HTTP 429")
	cmd.Flags().IntVar(&flags.requestTimeoutSeconds, "request-timeout", 0, "per-render request timeout in seconds; 0 uses job/runtime timeout only")
	cmd.Flags().StringVar(&flags.rendererCommand, "renderer-command", "", "renderer command override")
	cmd.Flags().StringVar(&flags.workerCommand, "worker-command", "", "persistent renderer worker command override")
	cmd.Flags().StringVar(&flags.jobStore, "job-store", "", "directory for persisted async job records")
	cmd.Flags().StringVar(&flags.jobTTL, "job-ttl", "", "prune persisted job records older than this duration on startup, e.g. 24h")
	cmd.Flags().StringVar(&flags.authToken, "auth-token", "", "protect non-health HTTP endpoints with this bearer token; also supports MOLSTAR_AUTH_TOKEN")
	cmd.Flags().BoolVar(&flags.openapi, "openapi", false, "write the HTTP OpenAPI schema to stdout and exit")
	cmd.Flags().BoolVar(&flags.prewarm, "prewarm", false, "start the renderer and run a tiny local render probe before accepting traffic")
	cmd.Flags().BoolVar(&flags.foregroundReport, "foreground-report", false, "print a detailed startup report before serving")
	cmd.Flags().BoolVar(&flags.noWorker, "no-worker", false, "disable persistent renderer workers and spawn one renderer process per job")
	cmd.Flags().BoolVar(&flags.dryRun, "dry-run", false, "return renderer commands without running them")
	cmd.Flags().BoolVar(&flags.quiet, "quiet", false, "suppress renderer progress logs")
	cmd.Flags().BoolVar(&flags.verbose, "verbose", false, "show renderer progress logs")
	bindRuntimeFlags(cmd, &flags.runtime)
	cmd.AddCommand(a.serveSmokeCommand())
	return cmd
}

func (a app) runServe(ctx context.Context, flags *serveFlags, cmd *cobra.Command) error {
	if strings.TrimSpace(flags.jobStore) != "" {
		if err := os.MkdirAll(flags.jobStore, 0o755); err != nil {
			return markError(kindRuntime, err)
		}
		if strings.TrimSpace(flags.jobTTL) != "" {
			ttl, err := time.ParseDuration(flags.jobTTL)
			if err != nil {
				return markError(kindInvalidInput, err)
			}
			if _, err := pruneJobStore(flags.jobStore, ttl, time.Now(), false); err != nil {
				return markError(kindRuntime, err)
			}
		}
	}
	network := "tcp"
	address := flags.addr
	if strings.TrimSpace(flags.socket) != "" {
		network = "unix"
		address = flags.socket
		if err := os.MkdirAll(filepath.Dir(address), 0o755); err != nil {
			return markError(kindRuntime, err)
		}
		_ = os.Remove(address)
	}
	listener, err := net.Listen(network, address)
	if err != nil {
		return markError(kindRuntime, err)
	}
	defer listener.Close()
	if network == "unix" {
		defer os.Remove(address)
	}

	gate := newRenderGate(flags.workers, flags.queue)
	store := newRenderJobStore(a, flags, gate)
	defer store.close()
	if flags.prewarm {
		report, err := store.prewarm(ctx)
		if err != nil {
			return markError(kindRuntime, err)
		}
		if !flags.quiet {
			fmt.Fprintf(a.stderr, "prewarm ok: %d stage(s), %d command(s)\n", len(report.Stages), len(report.Commands))
		}
	}
	server := &http.Server{
		Handler:           a.serveMux(flags, cmd, gate, store),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	if flags.foregroundReport {
		a.writeServeForegroundReport(flags, cmd, network, listener.Addr().String(), store)
	} else if network == "unix" {
		fmt.Fprintf(a.stdout, "molstar serve listening on unix://%s\n", address)
	} else {
		fmt.Fprintf(a.stdout, "molstar serve listening on http://%s\n", listener.Addr().String())
	}
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return markError(kindRuntime, err)
	}
	return nil
}

func (a app) writeServeForegroundReport(flags *serveFlags, cmd *cobra.Command, network string, address string, store *renderJobStore) {
	endpoint := "http://" + address
	if network == "unix" {
		endpoint = "unix://" + flags.socket
	}
	auth := "disabled"
	if strings.TrimSpace(flags.authToken) != "" || strings.TrimSpace(os.Getenv("MOLSTAR_AUTH_TOKEN")) != "" {
		auth = "enabled"
	}
	rendererMode := "worker"
	if flags.noWorker {
		rendererMode = "subprocess"
	}
	runtime := job.ApplyRuntimeProfile(runtimeFromFlags(cmd, job.Runtime{}, flags.runtime))
	fmt.Fprintf(a.stdout, "molstar serve\t%s\n", endpoint)
	fmt.Fprintf(a.stdout, "health\t%s\n", serveEndpoint(endpoint, "/health"))
	fmt.Fprintf(a.stdout, "ready\t%s\n", serveEndpoint(endpoint, "/ready"))
	fmt.Fprintf(a.stdout, "auth\t%s\n", auth)
	fmt.Fprintf(a.stdout, "renderer\t%s\n", rendererMode)
	fmt.Fprintf(a.stdout, "workers\t%d\n", flags.workers)
	fmt.Fprintf(a.stdout, "queue\t%d\n", flags.queue)
	if runtime.Cache != "" {
		fmt.Fprintf(a.stdout, "cache\t%s\n", runtime.Cache)
	}
	if store.dir != "" {
		fmt.Fprintf(a.stdout, "job-store\t%s\n", store.dir)
	}
}

func serveEndpoint(base string, suffix string) string {
	if strings.HasPrefix(base, "unix://") {
		return base + " " + suffix
	}
	return strings.TrimRight(base, "/") + suffix
}

func (a app) serveHandler(flags *serveFlags, cmd *cobra.Command) http.Handler {
	gate := newRenderGate(flags.workers, flags.queue)
	store := newRenderJobStore(a, flags, gate)
	return a.serveMux(flags, cmd, gate, store)
}

func (a app) serveMux(flags *serveFlags, cmd *cobra.Command, gate *renderGate, store *renderJobStore) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{
			"ok":                true,
			"service":           "headlessmolstar",
			"time":              time.Now().UTC().Format(time.RFC3339),
			"workers":           gate.workers,
			"queue":             gate.queue,
			"active_jobs":       gate.active.Load(),
			"queued_jobs":       gate.queued(),
			"worker_capacity":   gate.workerCapacity(),
			"inflight_capacity": gate.capacity(),
			"renderer_workers":  store.workerStatus(),
			"ready":             store.readiness(),
			"metrics":           store.metricsSnapshot(),
			"job_store":         store.dir,
			"job_ttl":           flags.jobTTL,
			"stored_jobs":       store.count(),
		})
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		ready := store.readiness()
		status := http.StatusOK
		if ok, _ := ready["ok"].(bool); !ok {
			status = http.StatusServiceUnavailable
		}
		writeHTTPJSON(w, status, ready)
	})
	mux.HandleFunc("/schema", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		writeHTTPJSON(w, http.StatusOK, job.JSONSchema())
	})
	mux.HandleFunc("/schema/openapi", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		writeHTTPJSON(w, http.StatusOK, serveOpenAPISchema())
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		writeHTTPJSON(w, http.StatusOK, store.metricsSnapshot())
	})
	mux.HandleFunc("/metrics/prometheus", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		writePrometheusMetrics(w, store.metricsSnapshot())
	})
	mux.HandleFunc("/capabilities", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		writeHTTPJSON(w, http.StatusOK, store.capabilities(r.Context(), cmd))
	})
	mux.HandleFunc("/validate", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		j, err := a.readHTTPJob(w, r, flags, cmd, queryBool(r, "strict"))
		if err != nil {
			writeHTTPError(w, "validate", err)
			return
		}
		compiled, err := mvs.Compile(j)
		if err != nil {
			writeHTTPError(w, "validate", markError(kindInvalidScene, err))
			return
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{
			"ok":       true,
			"warnings": compiled.Warnings,
		})
	})
	mux.HandleFunc("/explain", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		j, err := a.readHTTPJob(w, r, flags, cmd, false)
		if err != nil {
			writeHTTPError(w, "explain", err)
			return
		}
		writeHTTPJSON(w, http.StatusOK, explainJobReport(j))
	})
	mux.HandleFunc("/render", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		j, err := a.readHTTPJob(w, r, flags, cmd, false)
		if err != nil {
			writeHTTPError(w, "render", err)
			return
		}
		dryRun := flags.dryRun || queryBool(r, "dry_run")
		requestCtx := r.Context()
		if flags.requestTimeoutSeconds > 0 {
			var cancel context.CancelFunc
			requestCtx, cancel = context.WithTimeout(requestCtx, time.Duration(flags.requestTimeoutSeconds)*time.Second)
			defer cancel()
		}
		submitted, err := store.submit(j, dryRun)
		if err != nil {
			body := newErrorBody(err)
			body.Message = "render worker queue is full or the request timed out waiting for a worker"
			writeHTTPJSON(w, http.StatusTooManyRequests, map[string]any{
				"ok":        false,
				"command":   "render",
				"error":     body,
				"timestamp": time.Now().UTC().Format(time.RFC3339),
			})
			return
		}
		if queryBool(r, "async") {
			writeHTTPJSON(w, http.StatusAccepted, submitted.snapshot())
			return
		}
		select {
		case <-submitted.done:
			status := http.StatusOK
			if submitted.Error != nil {
				status = statusForError(kindError{kind: errorKind(submitted.Error.Code), err: errors.New(submitted.Error.Message)})
			}
			writeHTTPJSON(w, status, submitted.snapshot())
		case <-requestCtx.Done():
			store.cancel(submitted)
			writeHTTPError(w, "render", markError(kindCanceled, requestCtx.Err()))
		}
	})
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		a.handleJSONRPC(w, r, flags, cmd, store)
	})
	mux.HandleFunc("/jobs/", func(w http.ResponseWriter, r *http.Request) {
		clean := strings.Trim(path.Clean(strings.TrimPrefix(r.URL.Path, "/jobs/")), "/")
		parts := strings.Split(clean, "/")
		if clean == "" || len(parts) > 3 {
			writeHTTPJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "job not found"})
			return
		}
		current := store.get(parts[0])
		if current == nil {
			writeHTTPJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "job not found"})
			return
		}
		if len(parts) >= 2 {
			switch parts[1] {
			case "events":
				if len(parts) != 2 {
					writeHTTPJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "job not found"})
					return
				}
				if !requireMethod(w, r, http.MethodGet) {
					return
				}
				writeHTTPEvents(w, current)
				return
			case "outputs":
				if !requireMethod(w, r, http.MethodGet) {
					return
				}
				if len(parts) == 2 {
					writeHTTPJSON(w, http.StatusOK, map[string]any{"ok": true, "id": parts[0], "outputs": current.outputFiles()})
					return
				}
				writeHTTPOutput(w, current, parts[2])
				return
			default:
				writeHTTPJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "job not found"})
				return
			}
		}
		switch r.Method {
		case http.MethodGet:
			writeHTTPJSON(w, http.StatusOK, current.snapshot())
		case http.MethodDelete:
			store.cancel(current)
			writeHTTPJSON(w, http.StatusAccepted, current.snapshot())
		default:
			w.Header().Set("Allow", "GET, DELETE")
			writeHTTPJSON(w, http.StatusMethodNotAllowed, map[string]any{"ok": false, "error": "method not allowed"})
		}
	})
	return authHTTPHandler(flags, mux)
}

type renderGate struct {
	workers    int
	queue      int
	queueSlots chan struct{}
	workSlots  chan struct{}
	active     atomic.Int64
}

type renderJobStore struct {
	app             app
	flags           *serveFlags
	gate            *renderGate
	rendererCommand string
	workerCommand   []string
	useWorkers      bool
	dir             string

	mu         sync.Mutex
	nextID     atomic.Int64
	jobs       map[string]*serverRenderJob
	order      []string
	workerPool *render.WorkerPool
	ready      serverReadiness
	metrics    *serverMetrics
}

// maxRetainedJobs bounds how many finished job records are kept in memory. A
// client issuing many /render calls would otherwise grow the jobs map without
// limit; the oldest terminal jobs are evicted from memory (persisted records on
// disk are pruned separately by --job-ttl).
const maxRetainedJobs = 1024

type serverRenderJob struct {
	ID          string        `json:"id"`
	Status      string        `json:"status"`
	SubmittedAt string        `json:"submitted_at"`
	StartedAt   string        `json:"started_at,omitempty"`
	FinishedAt  string        `json:"finished_at,omitempty"`
	Events      []serverEvent `json:"events,omitempty"`
	Report      *renderReport `json:"report,omitempty"`
	Error       *errorBody    `json:"error,omitempty"`
	done        chan struct{} `json:"-"`
	cancel      context.CancelFunc
	ctx         context.Context
	spec        job.Job
	dryRun      bool
	persisted   bool
	submittedAt time.Time
	startedAt   time.Time
	mu          sync.Mutex
}

type serverEvent struct {
	Time    string `json:"time"`
	Phase   string `json:"phase"`
	Message string `json:"message,omitempty"`
}

type serverReadiness struct {
	OK        bool       `json:"ok"`
	Required  bool       `json:"required"`
	CheckedAt string     `json:"checked_at,omitempty"`
	Error     *errorBody `json:"error,omitempty"`
	Stages    int        `json:"stages,omitempty"`
	Commands  int        `json:"commands,omitempty"`
}

type serverMetrics struct {
	startedAt time.Time

	submitted       atomic.Int64
	rejected        atomic.Int64
	started         atomic.Int64
	succeeded       atomic.Int64
	failed          atomic.Int64
	canceled        atomic.Int64
	workerFallbacks atomic.Int64

	totalQueueWaitMS      atomic.Int64
	maxQueueWaitMS        atomic.Int64
	totalRenderDurationMS atomic.Int64
	maxRenderDurationMS   atomic.Int64

	mu             sync.Mutex
	failuresByCode map[string]int64
	queueWaitHist  map[string]int64
	renderHist     map[string]int64
}

func newServerMetrics() *serverMetrics {
	return &serverMetrics{
		startedAt:      time.Now(),
		failuresByCode: map[string]int64{},
		queueWaitHist:  newHistogramCounts(),
		renderHist:     newHistogramCounts(),
	}
}

func (m *serverMetrics) recordSubmitted() {
	m.submitted.Add(1)
}

func (m *serverMetrics) recordRejected() {
	m.rejected.Add(1)
}

func (m *serverMetrics) recordStarted(queueWait time.Duration) {
	m.started.Add(1)
	queueWaitMS := durationMS(queueWait)
	m.totalQueueWaitMS.Add(queueWaitMS)
	storeMaxInt64(&m.maxQueueWaitMS, queueWaitMS)
	m.mu.Lock()
	recordHistogramMS(m.queueWaitHist, queueWaitMS)
	m.mu.Unlock()
}

func (m *serverMetrics) recordWorkerFallback() {
	m.workerFallbacks.Add(1)
}

func (m *serverMetrics) recordFinished(status string, code errorKind, duration time.Duration) {
	switch status {
	case "succeeded":
		m.succeeded.Add(1)
	case "canceled":
		m.canceled.Add(1)
	default:
		m.failed.Add(1)
	}
	durationMS := durationMS(duration)
	m.totalRenderDurationMS.Add(durationMS)
	storeMaxInt64(&m.maxRenderDurationMS, durationMS)
	m.mu.Lock()
	recordHistogramMS(m.renderHist, durationMS)
	if code != "" && code != kindCanceled {
		m.failuresByCode[string(code)]++
	}
	m.mu.Unlock()
}

func (m *serverMetrics) snapshot(gate *renderGate, workerPool map[string]any) map[string]any {
	submitted := m.submitted.Load()
	started := m.started.Load()
	succeeded := m.succeeded.Load()
	failed := m.failed.Load()
	canceled := m.canceled.Load()
	finished := succeeded + failed + canceled

	m.mu.Lock()
	failuresByCode := make(map[string]int64, len(m.failuresByCode))
	for code, count := range m.failuresByCode {
		failuresByCode[code] = count
	}
	queueWaitHist := copyInt64Map(m.queueWaitHist)
	renderHist := copyInt64Map(m.renderHist)
	m.mu.Unlock()

	report := map[string]any{
		"ok":              true,
		"started_at":      m.startedAt.UTC().Format(time.RFC3339Nano),
		"uptime_ms":       time.Since(m.startedAt).Milliseconds(),
		"submitted":       submitted,
		"rejected":        m.rejected.Load(),
		"started":         started,
		"succeeded":       succeeded,
		"failed":          failed,
		"canceled":        canceled,
		"finished":        finished,
		"active_jobs":     gate.active.Load(),
		"queued_jobs":     gate.queued(),
		"worker_capacity": gate.workerCapacity(),
		"queue_capacity":  gate.capacity(),
		"queue_wait_ms": map[string]any{
			"avg":     averageInt64(m.totalQueueWaitMS.Load(), started),
			"max":     m.maxQueueWaitMS.Load(),
			"sum":     m.totalQueueWaitMS.Load(),
			"count":   started,
			"buckets": queueWaitHist,
		},
		"render_duration_ms": map[string]any{
			"avg":     averageInt64(m.totalRenderDurationMS.Load(), finished),
			"max":     m.maxRenderDurationMS.Load(),
			"sum":     m.totalRenderDurationMS.Load(),
			"count":   finished,
			"buckets": renderHist,
		},
		"failures_by_error_code": failuresByCode,
		"worker_fallbacks":       m.workerFallbacks.Load(),
	}
	if workerPool != nil {
		report["worker_pool"] = workerPool
	}
	return report
}

var prometheusHistogramBucketsMS = []int64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000}

func newHistogramCounts() map[string]int64 {
	counts := make(map[string]int64, len(prometheusHistogramBucketsMS)+1)
	for _, bucket := range prometheusHistogramBucketsMS {
		counts[fmt.Sprintf("%d", bucket)] = 0
	}
	counts["+Inf"] = 0
	return counts
}

func recordHistogramMS(counts map[string]int64, value int64) {
	for _, bucket := range prometheusHistogramBucketsMS {
		if value <= bucket {
			counts[fmt.Sprintf("%d", bucket)]++
		}
	}
	counts["+Inf"]++
}

func copyInt64Map(input map[string]int64) map[string]int64 {
	output := make(map[string]int64, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func storeMaxInt64(target *atomic.Int64, value int64) {
	for {
		current := target.Load()
		if value <= current {
			return
		}
		if target.CompareAndSwap(current, value) {
			return
		}
	}
}

func durationMS(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	return duration.Milliseconds()
}

func averageInt64(total, count int64) float64 {
	if count <= 0 {
		return 0
	}
	return float64(total) / float64(count)
}

func newRenderJobStore(a app, flags *serveFlags, gate *renderGate) *renderJobStore {
	store := &renderJobStore{
		app:             a,
		flags:           flags,
		gate:            gate,
		rendererCommand: flags.rendererCommand,
		workerCommand:   strings.Fields(flags.workerCommand),
		useWorkers:      !flags.noWorker && flags.rendererCommand == "",
		dir:             strings.TrimSpace(flags.jobStore),
		jobs:            map[string]*serverRenderJob{},
		ready:           serverReadiness{OK: !flags.prewarm, Required: flags.prewarm},
		metrics:         newServerMetrics(),
	}
	store.loadPersistedJobs()
	return store
}

func (s *renderJobStore) submit(spec job.Job, dryRun bool) (*serverRenderJob, error) {
	if !s.gate.tryQueue() {
		s.metrics.recordRejected()
		return nil, markError(kindServerBusy, fmt.Errorf("render worker queue is full"))
	}
	now := time.Now()
	id := fmt.Sprintf("job_%d", s.nextID.Add(1))
	ctx, cancel := context.WithCancel(context.Background())
	current := &serverRenderJob{
		ID:          id,
		Status:      "queued",
		SubmittedAt: now.UTC().Format(time.RFC3339Nano),
		done:        make(chan struct{}),
		cancel:      cancel,
		ctx:         ctx,
		spec:        spec,
		dryRun:      dryRun,
		submittedAt: now,
	}
	s.metrics.recordSubmitted()
	current.addEvent("queued", "job accepted")
	s.mu.Lock()
	s.jobs[id] = current
	s.order = append(s.order, id)
	s.evictTerminalLocked()
	s.mu.Unlock()
	s.persist(current)
	go s.run(current)
	return current, nil
}

// evictTerminalLocked drops the oldest terminal jobs from memory when the
// retained-job count exceeds the cap. Running/queued jobs are never evicted, so
// an in-flight job a client is polling is always preserved. Caller holds s.mu.
func (s *renderJobStore) evictTerminalLocked() {
	if len(s.jobs) <= maxRetainedJobs {
		return
	}
	kept := s.order[:0]
	for _, id := range s.order {
		current, ok := s.jobs[id]
		if len(s.jobs) > maxRetainedJobs && ok && current.isTerminal() {
			delete(s.jobs, id)
			continue
		}
		if ok {
			kept = append(kept, id)
		}
	}
	s.order = kept
}

func isTerminalJobStatus(status string) bool {
	switch status {
	case "succeeded", "failed", "canceled":
		return true
	default:
		return false
	}
}

// isTerminal reports whether the job has reached a terminal status. It takes the
// job's own lock; callers may hold the store lock (ordering is store -> job).
func (j *serverRenderJob) isTerminal() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return isTerminalJobStatus(j.Status)
}

func (s *renderJobStore) get(id string) *serverRenderJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jobs[id]
}

func (s *renderJobStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.jobs)
}

func (s *renderJobStore) close() {
	s.mu.Lock()
	pool := s.workerPool
	s.workerPool = nil
	s.mu.Unlock()
	if pool != nil {
		_ = pool.Close()
	}
}

func (s *renderJobStore) workerStatus() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := map[string]any{
		"enabled": s.useWorkers,
		"started": s.workerPool != nil,
	}
	if s.workerPool != nil {
		status["pool"] = s.workerPool.Stats()
	}
	if len(s.workerCommand) > 0 {
		status["command"] = append([]string{}, s.workerCommand...)
	}
	return status
}

func (s *renderJobStore) metricsSnapshot() map[string]any {
	s.mu.Lock()
	pool := s.workerPool
	s.mu.Unlock()
	var workerPool map[string]any
	if pool != nil {
		workerPool = pool.Stats()
	}
	return s.metrics.snapshot(s.gate, workerPool)
}

func writePrometheusMetrics(w http.ResponseWriter, metrics map[string]any) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	writePrometheusMetric(w, "headlessmolstar_jobs_submitted_total", "counter", "Total submitted render jobs.", metricFloat(metrics, "submitted"))
	writePrometheusMetric(w, "headlessmolstar_jobs_rejected_total", "counter", "Total rejected render jobs.", metricFloat(metrics, "rejected"))
	writePrometheusMetric(w, "headlessmolstar_jobs_started_total", "counter", "Total started render jobs.", metricFloat(metrics, "started"))
	writePrometheusMetric(w, "headlessmolstar_jobs_succeeded_total", "counter", "Total succeeded render jobs.", metricFloat(metrics, "succeeded"))
	writePrometheusMetric(w, "headlessmolstar_jobs_failed_total", "counter", "Total failed render jobs.", metricFloat(metrics, "failed"))
	writePrometheusMetric(w, "headlessmolstar_jobs_canceled_total", "counter", "Total canceled render jobs.", metricFloat(metrics, "canceled"))
	writePrometheusGauge(w, "headlessmolstar_jobs_active", "Currently active render jobs.", metricFloat(metrics, "active_jobs"))
	writePrometheusGauge(w, "headlessmolstar_jobs_queued", "Currently queued render jobs.", metricFloat(metrics, "queued_jobs"))
	writePrometheusGauge(w, "headlessmolstar_worker_capacity", "Configured render worker capacity.", metricFloat(metrics, "worker_capacity"))
	writePrometheusGauge(w, "headlessmolstar_queue_capacity", "Configured queue capacity.", metricFloat(metrics, "queue_capacity"))
	writePrometheusGauge(w, "headlessmolstar_queue_wait_avg_ms", "Average queue wait in milliseconds.", nestedMetricFloat(metrics, "queue_wait_ms", "avg"))
	writePrometheusGauge(w, "headlessmolstar_queue_wait_max_ms", "Maximum queue wait in milliseconds.", nestedMetricFloat(metrics, "queue_wait_ms", "max"))
	writePrometheusGauge(w, "headlessmolstar_render_duration_avg_ms", "Average render duration in milliseconds.", nestedMetricFloat(metrics, "render_duration_ms", "avg"))
	writePrometheusGauge(w, "headlessmolstar_render_duration_max_ms", "Maximum render duration in milliseconds.", nestedMetricFloat(metrics, "render_duration_ms", "max"))
	writePrometheusHistogram(w, "headlessmolstar_queue_wait_ms", "Queue wait duration in milliseconds.", nestedMetricMap(metrics, "queue_wait_ms", "buckets"), nestedMetricFloat(metrics, "queue_wait_ms", "sum"), nestedMetricFloat(metrics, "queue_wait_ms", "count"))
	writePrometheusHistogram(w, "headlessmolstar_render_duration_ms", "Render duration in milliseconds.", nestedMetricMap(metrics, "render_duration_ms", "buckets"), nestedMetricFloat(metrics, "render_duration_ms", "sum"), nestedMetricFloat(metrics, "render_duration_ms", "count"))
	writePrometheusMetric(w, "headlessmolstar_worker_fallbacks_total", "counter", "Total worker fallback events.", metricFloat(metrics, "worker_fallbacks"))
	if failures, ok := metrics["failures_by_error_code"].(map[string]int64); ok {
		for code, count := range failures {
			writePrometheusLabeledMetric(w, "headlessmolstar_failures_total", "counter", "Total render failures by error code.", map[string]string{"code": code}, float64(count))
		}
	}
	if failures, ok := metrics["failures_by_error_code"].(map[string]any); ok {
		for code, count := range failures {
			writePrometheusLabeledMetric(w, "headlessmolstar_failures_total", "counter", "Total render failures by error code.", map[string]string{"code": code}, anyMetricFloat(count))
		}
	}
}

func writePrometheusGauge(w io.Writer, name string, help string, value float64) {
	writePrometheusMetric(w, name, "gauge", help, value)
}

func writePrometheusMetric(w io.Writer, name string, kind string, help string, value float64) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s %s\n", name, kind)
	fmt.Fprintf(w, "%s %g\n", name, value)
}

func writePrometheusLabeledMetric(w io.Writer, name string, kind string, help string, labels map[string]string, value float64) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s %s\n", name, kind)
	fmt.Fprintf(w, "%s{", name)
	first := true
	for key, labelValue := range labels {
		if !first {
			fmt.Fprint(w, ",")
		}
		first = false
		fmt.Fprintf(w, "%s=%q", key, labelValue)
	}
	fmt.Fprintf(w, "} %g\n", value)
}

func writePrometheusHistogram(w io.Writer, name string, help string, buckets map[string]float64, sum float64, count float64) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s histogram\n", name)
	for _, bucket := range prometheusHistogramBucketsMS {
		le := fmt.Sprintf("%d", bucket)
		fmt.Fprintf(w, "%s_bucket{le=%q} %g\n", name, le, buckets[le])
	}
	fmt.Fprintf(w, "%s_bucket{le=\"+Inf\"} %g\n", name, buckets["+Inf"])
	fmt.Fprintf(w, "%s_sum %g\n", name, sum)
	fmt.Fprintf(w, "%s_count %g\n", name, count)
}

func metricFloat(metrics map[string]any, key string) float64 {
	return anyMetricFloat(metrics[key])
}

func nestedMetricFloat(metrics map[string]any, key string, nestedKey string) float64 {
	nested, ok := metrics[key].(map[string]any)
	if !ok {
		return 0
	}
	return anyMetricFloat(nested[nestedKey])
}

func nestedMetricMap(metrics map[string]any, key string, nestedKey string) map[string]float64 {
	nested, ok := metrics[key].(map[string]any)
	if !ok {
		return map[string]float64{}
	}
	return metricFloatMap(nested[nestedKey])
}

func metricFloatMap(value any) map[string]float64 {
	result := map[string]float64{}
	switch typed := value.(type) {
	case map[string]int64:
		for key, entry := range typed {
			result[key] = float64(entry)
		}
	case map[string]any:
		for key, entry := range typed {
			result[key] = anyMetricFloat(entry)
		}
	}
	return result
}

func anyMetricFloat(value any) float64 {
	switch typed := value.(type) {
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case float64:
		return typed
	case json.Number:
		result, _ := typed.Float64()
		return result
	default:
		return 0
	}
}

func (s *renderJobStore) readiness() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	ready := map[string]any{
		"ok":       s.ready.OK,
		"required": s.ready.Required,
	}
	if s.ready.CheckedAt != "" {
		ready["checked_at"] = s.ready.CheckedAt
	}
	if s.ready.Error != nil {
		ready["error"] = s.ready.Error
	}
	if s.ready.Stages > 0 {
		ready["stages"] = s.ready.Stages
	}
	if s.ready.Commands > 0 {
		ready["commands"] = s.ready.Commands
	}
	return ready
}

func (s *renderJobStore) setReadiness(report *renderReport, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready.CheckedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err != nil {
		s.ready.OK = false
		body := newErrorBody(err)
		s.ready.Error = &body
		s.ready.Stages = 0
		s.ready.Commands = 0
		return
	}
	s.ready.OK = true
	s.ready.Error = nil
	if report != nil {
		s.ready.Stages = len(report.Stages)
		s.ready.Commands = len(report.Commands)
	}
}

func (s *renderJobStore) run(current *serverRenderJob) {
	defer close(current.done)
	defer current.cancel()
	defer s.gate.releaseQueue()
	if !s.gate.waitWorker(current.ctx) {
		current.fail(markError(kindCanceled, current.ctx.Err()))
		s.metrics.recordFinished("canceled", kindCanceled, 0)
		s.persist(current)
		return
	}
	defer func() {
		select {
		case <-s.gate.workSlots:
			s.gate.active.Add(-1)
		default:
		}
	}()
	current.start()
	s.metrics.recordStarted(time.Since(current.submittedAt))
	s.persist(current)
	renderStart := time.Now()
	pooled, workerErr := s.workerRenderer(current.dryRun)
	if workerErr != nil {
		s.metrics.recordWorkerFallback()
		current.addEvent("worker_fallback", workerErr.Error())
		s.persist(current)
	}
	report, err := s.app.executeRenderJobWithRenderer(current.ctx, "http:"+current.ID, current.spec, s.rendererCommand, current.dryRun, s.flags.quiet, s.flags.verbose, pooled)
	if err != nil {
		current.fail(err)
		status := "failed"
		if classifyError(err) == kindCanceled {
			status = "canceled"
		}
		s.metrics.recordFinished(status, classifyError(err), time.Since(renderStart))
		s.persist(current)
		return
	}
	if report.Diagnostics == nil {
		report.Diagnostics = map[string]any{}
	}
	if workerErr != nil {
		report.Diagnostics["renderer_mode"] = "subprocess"
		report.Diagnostics["worker_fallback"] = workerErr.Error()
	} else if pooled != nil {
		report.Diagnostics["renderer_mode"] = "worker"
	} else {
		report.Diagnostics["renderer_mode"] = "subprocess"
	}
	current.succeed(report)
	s.metrics.recordFinished("succeeded", "", time.Since(renderStart))
	s.persist(current)
}

func (s *renderJobStore) cancel(current *serverRenderJob) {
	current.cancelJob()
	s.persist(current)
}

func (s *renderJobStore) workerRenderer(dryRun bool) (render.ImageRenderer, error) {
	if dryRun || !s.useWorkers {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.workerPool != nil {
		return s.workerPool, nil
	}
	pool, err := render.NewWorkerPool(s.gate.workers, s.workerCommand, nil, s.app.stderr, nil)
	if err != nil {
		return nil, err
	}
	s.workerPool = pool
	return pool, nil
}

func (j *serverRenderJob) addEvent(phase, message string) {
	j.Events = append(j.Events, serverEvent{
		Time:    time.Now().UTC().Format(time.RFC3339Nano),
		Phase:   phase,
		Message: message,
	})
}

func (j *serverRenderJob) start() {
	j.mu.Lock()
	defer j.mu.Unlock()
	now := time.Now()
	j.Status = "running"
	j.StartedAt = now.UTC().Format(time.RFC3339Nano)
	j.startedAt = now
	j.addEvent("running", "render started")
}

func (j *serverRenderJob) succeed(report renderReport) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Status = "succeeded"
	j.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	j.Report = &report
	j.addEvent("succeeded", "render completed")
}

func (j *serverRenderJob) fail(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	status := "failed"
	if classifyError(err) == kindCanceled {
		status = "canceled"
	}
	j.Status = status
	j.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	body := newErrorBody(err)
	j.Error = &body
	j.addEvent(status, err.Error())
}

func (j *serverRenderJob) cancelJob() {
	if j.cancel != nil {
		j.cancel()
	}
	j.mu.Lock()
	if j.Status == "queued" {
		j.Status = "canceled"
		j.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		body := newErrorBody(markError(kindCanceled, fmt.Errorf("job canceled")))
		j.Error = &body
		j.addEvent("canceled", "job canceled")
	}
	j.mu.Unlock()
}

func (j *serverRenderJob) snapshot() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	value := map[string]any{
		"ok":           j.Error == nil,
		"id":           j.ID,
		"status":       j.Status,
		"submitted_at": j.SubmittedAt,
		"events":       append([]serverEvent{}, j.Events...),
	}
	if j.StartedAt != "" {
		value["started_at"] = j.StartedAt
	}
	if j.FinishedAt != "" {
		value["finished_at"] = j.FinishedAt
	}
	if j.Report != nil {
		value["report"] = j.Report
	}
	if j.Error != nil {
		value["error"] = j.Error
	}
	return value
}

func (j *serverRenderJob) outputFiles() []outputReport {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.Report == nil || len(j.Report.OutputFiles) == 0 {
		return nil
	}
	return append([]outputReport{}, j.Report.OutputFiles...)
}

func writeHTTPOutput(w http.ResponseWriter, current *serverRenderJob, indexValue string) {
	index, err := strconv.Atoi(indexValue)
	if err != nil || index < 0 {
		writeHTTPJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "output not found"})
		return
	}
	outputs := current.outputFiles()
	if index >= len(outputs) {
		writeHTTPJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "output not found"})
		return
	}
	output := outputs[index]
	file, err := os.Open(output.Path)
	if err != nil {
		writeHTTPJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		writeHTTPJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "output not found"})
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(output.Path)))
	w.Header().Set("X-Molstar-Output-Path", output.Path)
	w.Header().Set("X-Molstar-Output-Type", output.Type)
	http.ServeContent(w, &http.Request{Method: http.MethodGet}, filepath.Base(output.Path), info.ModTime(), file)
}

type serverJobRecord struct {
	ID          string        `json:"id"`
	Status      string        `json:"status"`
	SubmittedAt string        `json:"submitted_at"`
	StartedAt   string        `json:"started_at,omitempty"`
	FinishedAt  string        `json:"finished_at,omitempty"`
	Events      []serverEvent `json:"events,omitempty"`
	Report      *renderReport `json:"report,omitempty"`
	Error       *errorBody    `json:"error,omitempty"`
	Spec        job.Job       `json:"spec,omitempty"`
	DryRun      bool          `json:"dry_run,omitempty"`
}

func (j *serverRenderJob) record() serverJobRecord {
	j.mu.Lock()
	defer j.mu.Unlock()
	return serverJobRecord{
		ID:          j.ID,
		Status:      j.Status,
		SubmittedAt: j.SubmittedAt,
		StartedAt:   j.StartedAt,
		FinishedAt:  j.FinishedAt,
		Events:      append([]serverEvent{}, j.Events...),
		Report:      j.Report,
		Error:       j.Error,
		Spec:        j.spec,
		DryRun:      j.dryRun,
	}
}

func serverRenderJobFromRecord(record serverJobRecord) *serverRenderJob {
	done := make(chan struct{})
	close(done)
	submittedAt, _ := time.Parse(time.RFC3339Nano, record.SubmittedAt)
	startedAt, _ := time.Parse(time.RFC3339Nano, record.StartedAt)
	return &serverRenderJob{
		ID:          record.ID,
		Status:      record.Status,
		SubmittedAt: record.SubmittedAt,
		StartedAt:   record.StartedAt,
		FinishedAt:  record.FinishedAt,
		Events:      append([]serverEvent{}, record.Events...),
		Report:      record.Report,
		Error:       record.Error,
		done:        done,
		cancel:      func() {},
		ctx:         context.Background(),
		spec:        record.Spec,
		dryRun:      record.DryRun,
		persisted:   true,
		submittedAt: submittedAt,
		startedAt:   startedAt,
	}
}

func (s *renderJobStore) persist(current *serverRenderJob) {
	if s.dir == "" || current == nil {
		return
	}
	record := current.record()
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		fmt.Fprintln(s.app.stderr, "warning: persist job:", err)
		return
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		fmt.Fprintln(s.app.stderr, "warning: persist job:", err)
		return
	}
	path := filepath.Join(s.dir, record.ID+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		fmt.Fprintln(s.app.stderr, "warning: persist job:", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		fmt.Fprintln(s.app.stderr, "warning: persist job:", err)
	}
}

func (s *renderJobStore) loadPersistedJobs() {
	if s.dir == "" {
		return
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	var maxID int64
	for _, entry := range entries {
		// Skip directories, non-JSON files, and hidden/AppleDouble "._*" files
		// macOS leaves in a job store on a non-native filesystem; otherwise each
		// spurious file would log a load warning on every startup.
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		if err != nil {
			fmt.Fprintln(s.app.stderr, "warning: load persisted job:", err)
			continue
		}
		var record serverJobRecord
		if err := json.Unmarshal(data, &record); err != nil {
			fmt.Fprintln(s.app.stderr, "warning: load persisted job:", err)
			continue
		}
		if record.ID == "" {
			continue
		}
		current := serverRenderJobFromRecord(record)
		s.jobs[current.ID] = current
		if strings.HasPrefix(current.ID, "job_") {
			if id, err := strconv.ParseInt(strings.TrimPrefix(current.ID, "job_"), 10, 64); err == nil && id > maxID {
				maxID = id
			}
		}
	}
	if maxID > 0 {
		s.nextID.Store(maxID)
	}
}

func newRenderGate(workers int, queue int) *renderGate {
	if workers < 1 {
		workers = 1
	}
	if queue < 0 {
		queue = 0
	}
	return &renderGate{
		workers:    workers,
		queue:      queue,
		queueSlots: make(chan struct{}, workers+queue),
		workSlots:  make(chan struct{}, workers),
	}
}

func (g *renderGate) tryAcquire(ctx context.Context) bool {
	if !g.tryQueue() {
		return false
	}
	if g.waitWorker(ctx) {
		return true
	}
	g.releaseQueue()
	return false
}

func (g *renderGate) tryQueue() bool {
	select {
	case g.queueSlots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (g *renderGate) waitWorker(ctx context.Context) bool {
	select {
	case g.workSlots <- struct{}{}:
		g.active.Add(1)
		return true
	case <-ctx.Done():
		return false
	}
}

func (g *renderGate) queued() int {
	queued := len(g.queueSlots) - len(g.workSlots)
	if queued < 0 {
		return 0
	}
	return queued
}

func (g *renderGate) capacity() int {
	return cap(g.queueSlots)
}

func (g *renderGate) workerCapacity() int {
	return cap(g.workSlots)
}

func (g *renderGate) release() {
	select {
	case <-g.workSlots:
		g.active.Add(-1)
	default:
	}
	g.releaseQueue()
}

func (g *renderGate) releaseQueue() {
	select {
	case <-g.queueSlots:
	default:
	}
}

func (a app) readHTTPJob(w http.ResponseWriter, r *http.Request, flags *serveFlags, cmd *cobra.Command, strict bool) (job.Job, error) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxHTTPJobBytes))
	if err != nil {
		return job.Job{}, markError(kindInvalidInput, err)
	}
	var j job.Job
	if strict {
		j, err = job.LoadSchemaRenderBytes(data, "request")
	} else {
		j, err = a.loadJobOrRecipeBytes(data, "request", true)
	}
	if err != nil {
		return job.Job{}, markError(kindInvalidInput, err)
	}
	applyRuntimeFlags(cmd, &j, flags.runtime)
	return j, nil
}

func (a app) executeRenderJob(ctx context.Context, input string, j job.Job, rendererCommand string, dryRun bool) (renderReport, error) {
	return a.executeRenderJobWithRenderer(ctx, input, j, rendererCommand, dryRun, false, false, nil)
}

func (a app) executeRenderJobWithRenderer(ctx context.Context, input string, j job.Job, rendererCommand string, dryRun bool, quiet bool, verbose bool, pooled render.ImageRenderer) (renderReport, error) {
	report := renderReport{OK: true, Input: input}
	if err := j.ValidateRender(); err != nil {
		return report, markError(kindInvalidInput, err)
	}
	stageStart := time.Now()
	prepared, runtimeReport, err := prepareJob(ctx, j)
	report.finishStage("prepare_runtime", input, stageStart, err)
	if err != nil {
		return report, markError(kindRuntime, err)
	}
	j = prepared
	report.CachedInputs = runtimeReport.CachedInputs
	jobCopy := j
	report.Job = &jobCopy

	stageStart = time.Now()
	compiled, err := mvs.Compile(j)
	report.finishStage("compile_mvs", "", stageStart, err)
	if err != nil {
		return report, markError(kindInvalidScene, err)
	}
	documentCopy := compiled.Document
	report.MVSDocument = &documentCopy
	report.Warnings = append(report.Warnings, compiled.Warnings...)
	report.Themes = append(report.Themes, compiled.ThemeExtensions...)

	imageOutputs := imageOutputsFromJob(j)
	stateOut := ""
	for _, output := range j.Outputs {
		switch output.NormalizedType() {
		case "mvsj":
			if dryRun {
				fmt.Fprintln(a.stderr, "dry-run: would write", output.Path)
				report.Outputs = append(report.Outputs, output.Path)
				continue
			}
			stageStart := time.Now()
			outputReport, err := writeMVSJTransactional(output.Path, compiled.Document)
			report.finishStage("write_mvsj", output.Path, stageStart, err)
			if err != nil {
				return report, markError(kindRender, err)
			}
			report.Outputs = append(report.Outputs, output.Path)
			report.OutputFiles = append(report.OutputFiles, outputReport)
		case "mvsx":
			if dryRun {
				fmt.Fprintln(a.stderr, "dry-run: would write", output.Path)
				report.Outputs = append(report.Outputs, output.Path)
				continue
			}
			stageStart := time.Now()
			outputReport, err := writeMVSXTransactional(output.Path, j, compiled.Document)
			report.finishStage("write_mvsx", output.Path, stageStart, err)
			if err != nil {
				return report, markError(kindRender, err)
			}
			report.Outputs = append(report.Outputs, output.Path)
			report.OutputFiles = append(report.OutputFiles, outputReport)
		case "molj":
			if stateOut == "" {
				stateOut = output.Path
			}
		case "image", "video":
		default:
			return report, markError(kindInvalidInput, fmt.Errorf("outputs: unsupported output type %q", output.Type))
		}
	}
	if stateOut != "" && len(imageOutputs) == 0 {
		return report, markError(kindInvalidInput, fmt.Errorf("molj output requires at least one image or video output"))
	}
	if len(imageOutputs) == 0 {
		return report, nil
	}
	if err := (job.Job{Runtime: j.Runtime, Outputs: imageOutputs}).ValidateRuntimeLimits(); err != nil {
		return report, markError(kindRuntime, err)
	}

	data, err := mvs.Marshal(compiled.Document)
	if err != nil {
		return report, markError(kindInvalidScene, err)
	}
	stageStart = time.Now()
	scenePath, cleanup, err := writeTempMVS(data)
	report.finishStage("write_temp_mvs", "", stageStart, err)
	if err != nil {
		return report, markError(kindInvalidScene, err)
	}
	defer cleanup()
	report.MVS = scenePath

	var runner render.ImageRenderer = pooled
	if runner == nil {
		molstar := render.NewMolstar()
		molstar.Stdout = a.stderr
		molstar.Stderr = a.stderr
		molstar.DryRun = dryRun
		molstar.Quiet = quiet || !verbose
		if rendererCommand != "" {
			molstar.RendererCommand = strings.Fields(rendererCommand)
		}
		runner = molstar
	}
	if report.Diagnostics == nil {
		report.Diagnostics = map[string]any{}
	}
	if pooled != nil {
		report.Diagnostics["renderer_mode"] = "worker"
	} else {
		report.Diagnostics["renderer_mode"] = "subprocess"
	}

	ctx, cancel := contextWithRuntimeTimeout(ctx, j.Runtime)
	defer cancel()
	rendered := 0
	for _, output := range j.Outputs {
		if output.NormalizedType() != "image" && output.NormalizedType() != "video" {
			continue
		}
		saveMolj := stateOut != "" && rendered == 0
		stageStart := time.Now()
		result, outputReports, err := renderTransactional(ctx, runner, scenePath, output, saveMolj, stateOut, dryRun)
		report.finishStage("render_output", output.Path, stageStart, err)
		report.Commands = append(report.Commands, result)
		if err != nil {
			return report, markError(kindRender, err)
		}
		for _, outputReport := range outputReports {
			report.Outputs = append(report.Outputs, outputReport.Path)
			report.OutputFiles = append(report.OutputFiles, outputReport)
		}
		rendered++
	}
	return report, nil
}

func explainJobReport(j job.Job) map[string]any {
	compiled, compileErr := mvsCompileForExplain(j)
	report := map[string]any{
		"ok":            compileErr == nil,
		"runtime":       j.Runtime,
		"inputs":        job.ExplainRuntime(j),
		"outputs":       plannedOutputs(j),
		"mvs_nodes":     compiled,
		"schema":        "job-v1",
		"would_render":  len(imageOutputsFromJob(j)) > 0,
		"would_compile": true,
	}
	if compileErr != nil {
		report["error"] = compileErr.Error()
	}
	return report
}

func plannedOutputs(j job.Job) []map[string]any {
	outputs := make([]map[string]any, 0, len(j.Outputs))
	for _, output := range j.Outputs {
		kind := output.NormalizedType()
		planned := map[string]any{
			"type":        kind,
			"path":        output.Path,
			"transaction": kind == "image" || kind == "video",
		}
		switch kind {
		case "image", "video":
			width, height := output.SizeOrDefault(800, 800)
			planned["width"] = width
			planned["height"] = height
			planned["pixels"] = width * height
			if output.Transparent {
				planned["transparent"] = true
			}
			if output.Quality != "" {
				planned["quality"] = output.Quality
			}
		case "mvsj", "mvsx":
			planned["export"] = true
		case "molj":
			planned["requires_render"] = true
		}
		outputs = append(outputs, planned)
	}
	return outputs
}

func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	writeHTTPJSON(w, http.StatusMethodNotAllowed, map[string]any{
		"ok":    false,
		"error": "method not allowed",
	})
	return false
}

func writeHTTPJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeHTTPEvents(w http.ResponseWriter, current *serverRenderJob) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	current.mu.Lock()
	events := append([]serverEvent{}, current.Events...)
	current.mu.Unlock()
	for _, event := range events {
		_ = json.NewEncoder(w).Encode(event)
	}
}

func writeHTTPError(w http.ResponseWriter, command string, err error) {
	writeHTTPJSON(w, statusForError(err), errorReport{
		OK:        false,
		Command:   command,
		Error:     newErrorBody(err),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

func statusForError(err error) int {
	switch classifyError(err) {
	case kindInvalidInput:
		return http.StatusBadRequest
	case kindInvalidScene, kindValidation:
		return http.StatusUnprocessableEntity
	case kindRuntime, kindSecurity:
		return http.StatusForbidden
	case kindNetwork, kindRenderer, kindRendererABI, kindRender:
		return http.StatusBadGateway
	case kindServerBusy:
		return http.StatusTooManyRequests
	case kindCanceled:
		return 499
	default:
		return http.StatusInternalServerError
	}
}

func queryBool(r *http.Request, key string) bool {
	value := strings.ToLower(strings.TrimSpace(r.URL.Query().Get(key)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}
