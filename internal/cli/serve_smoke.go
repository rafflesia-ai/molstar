package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sacha-ichbiah/molstar/internal/job"
)

type serveSmokeCheck struct {
	Name              string `json:"name"`
	OK                bool   `json:"ok"`
	Status            int    `json:"status,omitempty"`
	JobID             string `json:"job_id,omitempty"`
	Outputs           int    `json:"outputs,omitempty"`
	DownloadedOutputs int    `json:"downloaded_outputs,omitempty"`
	Error             string `json:"error,omitempty"`
}

type serveSmokeReport struct {
	OK       bool              `json:"ok"`
	URL      string            `json:"url,omitempty"`
	Socket   string            `json:"socket,omitempty"`
	Checks   []serveSmokeCheck `json:"checks"`
	Duration int64             `json:"duration_ms,omitempty"`
}

type serveSmokeFlags struct {
	renderProbe   bool
	probeOutDir   string
	probeTimeout  time.Duration
	probeInterval time.Duration
}

func (a app) serveSmokeCommand() *cobra.Command {
	flags := defaultServerClientFlags()
	smokeFlags := serveSmokeFlags{}
	cmd := &cobra.Command{
		Use:   "smoke",
		Short: "Smoke-test a running Mol* server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("serve smoke", flags.jsonReport, func() error {
				if err := exactArgs(args, 0, "serve smoke"); err != nil {
					return markError(kindInvalidInput, err)
				}
				report := runServeSmoke(cmd.Context(), flags, smokeFlags)
				if flags.jsonReport {
					if err := writeJSON(a.stdout, report); err != nil {
						return markError(kindInternal, err)
					}
					if !report.OK {
						return alreadyReported(markError(kindRuntime, fmt.Errorf("serve smoke failed")))
					}
					return nil
				}
				for _, check := range report.Checks {
					status := "OK"
					if !check.OK {
						status = "FAIL"
					}
					fmt.Fprintf(a.stdout, "%-5s %s", status, check.Name)
					if check.Status != 0 {
						fmt.Fprintf(a.stdout, " (%d)", check.Status)
					}
					if check.Error != "" {
						fmt.Fprintf(a.stdout, " - %s", singleLine(check.Error))
					}
					fmt.Fprintln(a.stdout)
				}
				if !report.OK {
					return markError(kindRuntime, fmt.Errorf("serve smoke failed"))
				}
				return nil
			})
		},
	}
	bindServerClientFlags(cmd, flags)
	cmd.Flags().BoolVar(&smokeFlags.renderProbe, "render-probe", false, "submit a tiny render job and verify server job lifecycle/output download")
	cmd.Flags().StringVar(&smokeFlags.probeOutDir, "probe-out-dir", "", "directory for render-probe files; defaults to a temporary directory")
	cmd.Flags().DurationVar(&smokeFlags.probeTimeout, "probe-timeout", 60*time.Second, "maximum time to wait for --render-probe")
	cmd.Flags().DurationVar(&smokeFlags.probeInterval, "probe-interval", 250*time.Millisecond, "poll interval for --render-probe")
	return cmd
}

func runServeSmoke(ctx context.Context, flags *serverClientFlags, smokeFlags serveSmokeFlags) serveSmokeReport {
	start := time.Now()
	report := serveSmokeReport{OK: true, URL: flags.url, Socket: flags.socket}
	add := func(check serveSmokeCheck) {
		if !check.OK {
			report.OK = false
		}
		report.Checks = append(report.Checks, check)
	}
	add(serveSmokeHTTPCheck(ctx, flags, "health", http.MethodGet, "/health", true))
	add(serveSmokeHTTPCheck(ctx, flags, "ready", http.MethodGet, "/ready", true))
	add(serveSmokeHTTPCheck(ctx, flags, "capabilities", http.MethodGet, "/capabilities", true))
	add(serveSmokeHTTPCheck(ctx, flags, "schema", http.MethodGet, "/schema", false))
	add(serveSmokeHTTPCheck(ctx, flags, "metrics", http.MethodGet, "/metrics", true))
	add(serveSmokeRPCCheck(ctx, flags))
	if smokeFlags.renderProbe {
		add(serveSmokeRenderProbe(ctx, flags, smokeFlags))
	}
	report.Duration = time.Since(start).Milliseconds()
	return report
}

func serveSmokeHTTPCheck(ctx context.Context, flags *serverClientFlags, name string, method string, path string, requiresOK bool) serveSmokeCheck {
	data, status, err := serverClientRequest(ctx, flags, method, path, nil)
	check := serveSmokeCheck{Name: name, Status: status}
	if err != nil {
		check.Error = err.Error()
		return check
	}
	if status < 200 || status >= 300 {
		check.Error = fmt.Sprintf("HTTP %d: %s", status, singleLine(string(data)))
		return check
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		check.Error = "response is not JSON: " + err.Error()
		return check
	}
	if requiresOK {
		if payload["ok"] != true {
			check.Error = "response ok field is not true"
			return check
		}
	} else if len(payload) == 0 {
		check.Error = "response JSON object is empty"
		return check
	}
	check.OK = true
	return check
}

func serveSmokeRenderProbe(ctx context.Context, flags *serverClientFlags, smokeFlags serveSmokeFlags) serveSmokeCheck {
	check := serveSmokeCheck{Name: "render_probe"}
	dir := strings.TrimSpace(smokeFlags.probeOutDir)
	cleanup := func() {}
	if dir == "" {
		temp, err := os.MkdirTemp("", "headlessmolstar-serve-smoke-*")
		if err != nil {
			check.Error = err.Error()
			return check
		}
		dir = temp
		cleanup = func() { _ = os.RemoveAll(temp) }
	} else if err := os.MkdirAll(dir, 0o755); err != nil {
		check.Error = err.Error()
		return check
	}
	defer cleanup()

	modelPath := filepath.Join(dir, "serve-smoke.cif")
	outputPath := filepath.Join(dir, "serve-smoke.png")
	downloadDir := filepath.Join(dir, "downloads")
	if err := os.WriteFile(modelPath, []byte(oneAtomCIF), 0o644); err != nil {
		check.Error = err.Error()
		return check
	}
	probe := job.Job{
		Version: 1,
		Runtime: job.Runtime{
			Offline:    true,
			Strict:     true,
			AllowPaths: []string{dir},
		},
		Inputs: map[string]job.Input{
			"input": {Path: modelPath, Format: "mmcif"},
		},
		Scene: job.Scene{
			Canvas: job.Canvas{Background: "white"},
			Structures: []job.Structure{{
				Source: "input",
				Components: []job.Component{{
					Ref:            "all",
					Select:         "all",
					Representation: job.Representation{Type: "spacefill", Color: "#cc3399"},
				}},
			}},
			Camera: job.Camera{Focus: "all"},
		},
		Outputs: []job.Output{{Type: "image", Path: outputPath, Size: []int{64, 64}}},
	}
	body, err := json.Marshal(probe)
	if err != nil {
		check.Error = err.Error()
		return check
	}
	data, status, err := serverClientRequest(ctx, flags, http.MethodPost, "/render?async=true", body)
	check.Status = status
	if err != nil {
		check.Error = err.Error()
		return check
	}
	if status < 200 || status >= 300 {
		check.Error = fmt.Sprintf("HTTP %d: %s", status, singleLine(string(data)))
		return check
	}
	var submitted serverJobStatusEnvelope
	if err := json.Unmarshal(data, &submitted); err != nil {
		check.Error = "submit response is not JSON: " + err.Error()
		return check
	}
	check.JobID = submitted.ID
	if submitted.ID == "" {
		check.Error = "render probe submit response did not include a job id"
		return check
	}
	statusReport, downloads, err := serverWaitForJob(ctx, flags, submitted.ID, smokeFlags.probeInterval, smokeFlags.probeTimeout, true, downloadDir)
	if statusReport.Report != nil {
		check.Outputs = len(statusReport.Report.OutputFiles)
	}
	check.DownloadedOutputs = len(downloads)
	if err != nil {
		check.Error = err.Error()
		return check
	}
	if check.Outputs == 0 || check.DownloadedOutputs == 0 {
		check.Error = "render probe completed without downloadable outputs"
		return check
	}
	check.OK = true
	return check
}

func serveSmokeRPCCheck(ctx context.Context, flags *serverClientFlags) serveSmokeCheck {
	body := []byte(`{"jsonrpc":"2.0","method":"capabilities","id":1}`)
	data, status, err := serverClientRequest(ctx, flags, http.MethodPost, "/rpc", body)
	check := serveSmokeCheck{Name: "rpc_capabilities", Status: status}
	if err != nil {
		check.Error = err.Error()
		return check
	}
	if status < 200 || status >= 300 {
		check.Error = fmt.Sprintf("HTTP %d: %s", status, singleLine(string(data)))
		return check
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		check.Error = "response is not JSON: " + err.Error()
		return check
	}
	result, _ := payload["result"].(map[string]any)
	if result["ok"] != true {
		check.Error = "JSON-RPC result ok field is not true"
		return check
	}
	check.OK = true
	return check
}
