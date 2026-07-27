package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/molstar/internal/job"
	"github.com/rafflesia-ai/molstar/internal/mvs"
	"github.com/rafflesia-ai/molstar/internal/render"
)

func (s *renderJobStore) capabilities(ctx context.Context, cmd *cobra.Command) map[string]any {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	runner := render.NewMolstar()
	if s.rendererCommand != "" {
		runner.RendererCommand = strings.Fields(s.rendererCommand)
	}
	runtime := job.ApplyRuntimeProfile(runtimeFromFlags(cmd, job.Runtime{}, s.flags.runtime))
	report := map[string]any{
		"ok":                 true,
		"time":               time.Now().UTC().Format(time.RFC3339),
		"runtime":            runtime,
		"renderer":           runner.Capabilities(ctx),
		"renderer_workers":   s.workerStatus(),
		"ready":              s.readiness(),
		"metrics":            s.metricsSnapshot(),
		"worker_capacity":    s.gate.workerCapacity(),
		"inflight_capacity":  s.gate.capacity(),
		"active_jobs":        s.gate.active.Load(),
		"queued_jobs":        s.gate.queued(),
		"job_store":          s.dir,
		"stored_jobs":        s.count(),
		"security_profile":   runtime.Profile,
		"network_enabled":    job.NetworkEnabled(runtime),
		"offline":            runtime.Offline,
		"max_pixels":         runtime.MaxPixels,
		"max_atoms":          runtime.MaxAtoms,
		"max_outputs":        runtime.MaxOutputs,
		"max_download_bytes": runtime.MaxDownloadBytes,
		"max_archive_bytes":  runtime.MaxArchiveBytes,
	}
	if s.useWorkers {
		worker, err := s.workerRenderer(false)
		if err != nil {
			report["worker_error"] = err.Error()
		} else if pool, ok := worker.(*render.WorkerPool); ok {
			capabilities, err := pool.Capabilities(ctx)
			if err != nil {
				report["worker_error"] = err.Error()
			} else {
				report["worker_capabilities"] = capabilities
			}
		}
	}
	return report
}

func (s *renderJobStore) prewarm(ctx context.Context) (renderReport, error) {
	dir, err := os.MkdirTemp("", "headlessmolstar-prewarm-*")
	if err != nil {
		s.setReadiness(nil, err)
		return renderReport{}, err
	}
	defer os.RemoveAll(dir)
	j, err := prewarmJob(dir)
	if err != nil {
		s.setReadiness(nil, err)
		return renderReport{}, err
	}
	var pooled render.ImageRenderer
	if s.useWorkers {
		pooled, err = s.workerRenderer(false)
		if err != nil {
			s.setReadiness(nil, err)
			return renderReport{}, err
		}
	}
	report, err := s.app.executeRenderJobWithRenderer(ctx, "prewarm", j, s.rendererCommand, false, true, false, pooled)
	s.setReadiness(&report, err)
	return report, err
}

func prewarmJob(dir string) (job.Job, error) {
	cifPath := filepath.Join(dir, "one.cif")
	if err := os.WriteFile(cifPath, []byte(oneAtomCIF), 0o644); err != nil {
		return job.Job{}, err
	}
	return job.Job{
		Version: 1,
		Runtime: job.Runtime{Profile: "locked", AllowPaths: []string{dir}},
		Inputs: map[string]job.Input{
			"probe": {Path: cifPath, Format: "mmcif"},
		},
		Scene: job.Scene{
			Canvas: job.Canvas{Background: "white"},
			Structures: []job.Structure{{
				Source: "probe",
				Components: []job.Component{{
					Ref:            "atom",
					Select:         "all",
					Representation: job.Representation{Type: "spacefill", Color: "#cc3399"},
				}},
			}},
			Camera: job.Camera{Focus: "all"},
		},
		Outputs: []job.Output{{
			Type: "image",
			Path: filepath.Join(dir, "prewarm.png"),
			Size: []int{64, 64},
		}},
	}, nil
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (a app) handleJSONRPC(w http.ResponseWriter, r *http.Request, flags *serveFlags, cmd *cobra.Command, store *renderJobStore) {
	var request rpcRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxHTTPJobBytes)).Decode(&request); err != nil {
		writeHTTPJSON(w, http.StatusBadRequest, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: err.Error()}})
		return
	}
	result, err := a.dispatchJSONRPC(r.Context(), request.Method, request.Params, flags, cmd, store)
	response := rpcResponse{JSONRPC: "2.0", ID: request.ID}
	status := http.StatusOK
	if err != nil {
		response.Error = &rpcError{Code: rpcErrorCode(err), Message: err.Error()}
		status = statusForError(err)
	} else {
		response.Result = result
	}
	writeHTTPJSON(w, status, response)
}

func (a app) dispatchJSONRPC(ctx context.Context, method string, params json.RawMessage, flags *serveFlags, cmd *cobra.Command, store *renderJobStore) (any, error) {
	switch method {
	case "capabilities":
		return store.capabilities(ctx, cmd), nil
	case "metrics":
		return store.metricsSnapshot(), nil
	case "validate":
		j, strict, err := rpcJob(params)
		if err != nil {
			return nil, markError(kindInvalidInput, err)
		}
		if strict {
			data, _ := json.Marshal(j)
			if _, err := job.LoadSchemaRenderBytes(data, "rpc"); err != nil {
				return nil, markError(kindValidation, err)
			}
		}
		applyRuntimeFlags(cmd, &j, flags.runtime)
		compiled, err := mvs.Compile(j)
		if err != nil {
			return nil, markError(kindInvalidScene, err)
		}
		return map[string]any{"ok": true, "warnings": compiled.Warnings}, nil
	case "explain":
		j, _, err := rpcJob(params)
		if err != nil {
			return nil, markError(kindInvalidInput, err)
		}
		applyRuntimeFlags(cmd, &j, flags.runtime)
		return explainJobReport(j), nil
	case "render":
		j, _, err := rpcJob(params)
		if err != nil {
			return nil, markError(kindInvalidInput, err)
		}
		// Merge the operator's serve-time hardening (profile, offline,
		// allow-path/host, max-pixels/atoms/outputs) onto the job, matching REST
		// /render and the validate/explain RPC methods. Without this, a job
		// submitted via /rpc would bypass the server's runtime policy.
		applyRuntimeFlags(cmd, &j, flags.runtime)
		dryRun, async := rpcRenderOptions(params)
		submitted, err := store.submit(j, flags.dryRun || dryRun)
		if err != nil {
			return nil, err
		}
		if async {
			return submitted.snapshot(), nil
		}
		// Apply the operator's request-wait cap to the synchronous wait, matching
		// REST /render; without this a synchronous /rpc render ignores
		// --request-timeout and holds the connection until the job's own timeout.
		waitCtx := ctx
		if flags.requestTimeoutSeconds > 0 {
			var cancel context.CancelFunc
			waitCtx, cancel = context.WithTimeout(ctx, time.Duration(flags.requestTimeoutSeconds)*time.Second)
			defer cancel()
		}
		select {
		case <-submitted.done:
			if submitted.Error != nil {
				return submitted.snapshot(), markError(errorKind(submitted.Error.Code), errors.New(submitted.Error.Message))
			}
			return submitted.snapshot(), nil
		case <-waitCtx.Done():
			store.cancel(submitted)
			return nil, markError(kindCanceled, waitCtx.Err())
		}
	default:
		return nil, markError(kindInvalidInput, fmt.Errorf("%w %q", errRPCMethodNotFound, method))
	}
}

// errRPCMethodNotFound marks an unknown JSON-RPC method so the response can use
// the spec's -32601 (Method not found) code instead of -32602 (Invalid params).
var errRPCMethodNotFound = errors.New("unknown JSON-RPC method")

func rpcJob(params json.RawMessage) (job.Job, bool, error) {
	var envelope struct {
		Job    json.RawMessage `json:"job"`
		Strict bool            `json:"strict"`
	}
	if len(params) == 0 || string(params) == "null" {
		return job.Job{}, false, fmt.Errorf("params.job is required")
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return job.Job{}, false, err
	}
	data := envelope.Job
	if len(data) == 0 {
		data = params
	}
	j, err := job.LoadRenderBytes(data, "rpc")
	return j, envelope.Strict, err
}

func rpcRenderOptions(params json.RawMessage) (bool, bool) {
	var envelope struct {
		DryRun bool `json:"dry_run"`
		Async  bool `json:"async"`
	}
	_ = json.Unmarshal(params, &envelope)
	return envelope.DryRun, envelope.Async
}

func rpcErrorCode(err error) int {
	if errors.Is(err, errRPCMethodNotFound) {
		return -32601 // Method not found
	}
	switch classifyError(err) {
	case kindInvalidInput, kindValidation, kindInvalidScene:
		return -32602
	case kindCanceled:
		return -32800
	default:
		return -32000
	}
}
