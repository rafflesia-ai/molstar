package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type serverClientFlags struct {
	url        string
	socket     string
	token      string
	timeout    time.Duration
	jsonReport bool
}

func (a app) serverCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Submit and manage jobs on a running Mol* server",
	}
	cmd.AddCommand(a.serverSubmitCommand())
	cmd.AddCommand(a.serverStatusCommand())
	cmd.AddCommand(a.serverEventsCommand())
	cmd.AddCommand(a.serverLogsCommand())
	cmd.AddCommand(a.serverWaitCommand())
	cmd.AddCommand(a.serverCancelCommand())
	return cmd
}

func (a app) serverSubmitCommand() *cobra.Command {
	flags := defaultServerClientFlags()
	var dryRun bool
	var wait bool
	var interval time.Duration
	var waitTimeout time.Duration
	var downloadOutputs bool
	var outDir string
	cmd := &cobra.Command{
		Use:   "submit JOB",
		Short: "Submit a job or recipe to a running server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("server submit", flags.jsonReport, func() error {
				if err := exactArgs(args, 1, "server submit"); err != nil {
					return markError(kindInvalidInput, err)
				}
				data, name, err := a.readInput(args[0])
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				j, err := a.loadJobOrRecipeBytes(data, name, true)
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				body, err := json.Marshal(j)
				if err != nil {
					return markError(kindInternal, err)
				}
				path := "/render?async=true"
				if dryRun {
					path += "&dry_run=true"
				}
				response, err := serverClientDo(cmd.Context(), flags, http.MethodPost, path, body)
				if err != nil {
					return err
				}
				if wait {
					return a.writeServerSubmitAndWait(cmd.Context(), flags, response, interval, waitTimeout, downloadOutputs, outDir)
				}
				return a.writeServerResponse(response, flags.jsonReport, "submitted")
			})
		},
	}
	bindServerClientFlags(cmd, flags)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "ask the server to plan rendering without running the renderer")
	cmd.Flags().BoolVar(&wait, "wait", false, "wait for the submitted job to finish")
	cmd.Flags().DurationVar(&interval, "interval", 500*time.Millisecond, "poll interval for --wait")
	cmd.Flags().DurationVar(&waitTimeout, "wait-timeout", 0, "maximum time to wait with --wait; 0 waits forever")
	cmd.Flags().BoolVar(&downloadOutputs, "download-outputs", false, "download completed output files when used with --wait")
	cmd.Flags().StringVar(&outDir, "out-dir", ".", "directory for --download-outputs")
	return cmd
}

func (a app) serverStatusCommand() *cobra.Command {
	flags := defaultServerClientFlags()
	cmd := &cobra.Command{
		Use:   "status JOB_ID",
		Short: "Fetch server job status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("server status", flags.jsonReport, func() error {
				if err := exactArgs(args, 1, "server status"); err != nil {
					return markError(kindInvalidInput, err)
				}
				response, err := serverClientDo(cmd.Context(), flags, http.MethodGet, "/jobs/"+url.PathEscape(args[0]), nil)
				if err != nil {
					return err
				}
				return a.writeServerResponse(response, flags.jsonReport, "status")
			})
		},
	}
	bindServerClientFlags(cmd, flags)
	return cmd
}

func (a app) serverEventsCommand() *cobra.Command {
	flags := defaultServerClientFlags()
	cmd := &cobra.Command{
		Use:   "events JOB_ID",
		Short: "Fetch server job events",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("server events", flags.jsonReport, func() error {
				if err := exactArgs(args, 1, "server events"); err != nil {
					return markError(kindInvalidInput, err)
				}
				response, err := serverClientDo(cmd.Context(), flags, http.MethodGet, "/jobs/"+url.PathEscape(args[0])+"/events", nil)
				if err != nil {
					return err
				}
				if flags.jsonReport {
					events, err := parseJSONLEvents(response)
					if err != nil {
						return markError(kindRuntime, err)
					}
					return writeJSON(a.stdout, map[string]any{"ok": true, "id": args[0], "events": events})
				}
				_, err = a.stdout.Write(ensureTrailingNewline(response))
				return markError(kindRender, err)
			})
		},
	}
	bindServerClientFlags(cmd, flags)
	return cmd
}

func (a app) serverCancelCommand() *cobra.Command {
	flags := defaultServerClientFlags()
	cmd := &cobra.Command{
		Use:   "cancel JOB_ID",
		Short: "Cancel a queued or running server job",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("server cancel", flags.jsonReport, func() error {
				if err := exactArgs(args, 1, "server cancel"); err != nil {
					return markError(kindInvalidInput, err)
				}
				response, err := serverClientDo(cmd.Context(), flags, http.MethodDelete, "/jobs/"+url.PathEscape(args[0]), nil)
				if err != nil {
					return err
				}
				return a.writeServerResponse(response, flags.jsonReport, "canceled")
			})
		},
	}
	bindServerClientFlags(cmd, flags)
	return cmd
}

func (a app) serverWaitCommand() *cobra.Command {
	flags := defaultServerClientFlags()
	var interval time.Duration
	var waitTimeout time.Duration
	var downloadOutputs bool
	var outDir string
	cmd := &cobra.Command{
		Use:   "wait JOB_ID",
		Short: "Wait for a server job to finish",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("server wait", flags.jsonReport, func() error {
				if err := exactArgs(args, 1, "server wait"); err != nil {
					return markError(kindInvalidInput, err)
				}
				status, downloads, err := serverWaitForJob(cmd.Context(), flags, args[0], interval, waitTimeout, downloadOutputs, outDir)
				if flags.jsonReport {
					report := serverJobStatusReport(status, downloads)
					if writeErr := writeJSON(a.stdout, report); writeErr != nil {
						return markError(kindInternal, writeErr)
					}
					if err != nil {
						return alreadyReported(err)
					}
					return nil
				}
				if writeErr := a.writeServerWaitSummary(status, downloads); writeErr != nil {
					return writeErr
				}
				return err
			})
		},
	}
	bindServerClientWaitFlags(cmd, flags)
	cmd.Flags().DurationVar(&interval, "interval", 500*time.Millisecond, "poll interval")
	cmd.Flags().DurationVar(&waitTimeout, "timeout", 0, "maximum time to wait; 0 waits forever")
	cmd.Flags().BoolVar(&downloadOutputs, "download-outputs", false, "download completed output files from the server")
	cmd.Flags().StringVar(&outDir, "out-dir", ".", "directory for --download-outputs")
	return cmd
}

func (a app) serverLogsCommand() *cobra.Command {
	flags := defaultServerClientFlags()
	flags.jsonReport = false
	cmd := &cobra.Command{
		Use:   "logs JOB_ID",
		Short: "Show a readable server job timeline",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("server logs", flags.jsonReport, func() error {
				if err := exactArgs(args, 1, "server logs"); err != nil {
					return markError(kindInvalidInput, err)
				}
				status, err := serverFetchJobStatus(cmd.Context(), flags, args[0])
				if err != nil {
					return err
				}
				if flags.jsonReport {
					return writeJSON(a.stdout, serverJobLogReport(status))
				}
				return a.writeServerLogs(status)
			})
		},
	}
	bindServerClientWaitFlags(cmd, flags)
	return cmd
}

func defaultServerClientFlags() *serverClientFlags {
	baseURL := strings.TrimSpace(os.Getenv("MOLSTAR_SERVER_URL"))
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8080"
	}
	return &serverClientFlags{
		url:        baseURL,
		token:      strings.TrimSpace(os.Getenv("MOLSTAR_AUTH_TOKEN")),
		timeout:    30 * time.Second,
		jsonReport: true,
	}
}

func bindServerClientFlags(cmd *cobra.Command, flags *serverClientFlags) {
	cmd.Flags().StringVar(&flags.url, "url", flags.url, "server base URL")
	cmd.Flags().StringVar(&flags.socket, "socket", "", "connect to a server Unix socket instead of TCP")
	cmd.Flags().StringVar(&flags.token, "token", flags.token, "bearer auth token; defaults to MOLSTAR_AUTH_TOKEN")
	cmd.Flags().DurationVar(&flags.timeout, "timeout", flags.timeout, "HTTP request timeout")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", flags.jsonReport, "write JSON output")
}

func bindServerClientWaitFlags(cmd *cobra.Command, flags *serverClientFlags) {
	cmd.Flags().StringVar(&flags.url, "url", flags.url, "server base URL")
	cmd.Flags().StringVar(&flags.socket, "socket", "", "connect to a server Unix socket instead of TCP")
	cmd.Flags().StringVar(&flags.token, "token", flags.token, "bearer auth token; defaults to MOLSTAR_AUTH_TOKEN")
	cmd.Flags().DurationVar(&flags.timeout, "request-timeout", flags.timeout, "per-HTTP-request timeout")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", flags.jsonReport, "write JSON output")
}

func serverClientDo(ctx context.Context, flags *serverClientFlags, method string, path string, body []byte) ([]byte, error) {
	data, status, err := serverClientRequest(ctx, flags, method, path, body)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, markError(kindRuntime, fmt.Errorf("server returned HTTP %d: %s", status, singleLine(string(data))))
	}
	return data, nil
}

func serverClientRequest(ctx context.Context, flags *serverClientFlags, method string, path string, body []byte) ([]byte, int, error) {
	if flags.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, flags.timeout)
		defer cancel()
	}
	client := &http.Client{}
	if strings.TrimSpace(flags.socket) != "" {
		socketPath := flags.socket
		client.Transport = &http.Transport{
			DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		}
	}
	target, err := serverClientURL(flags, path)
	if err != nil {
		return nil, 0, markError(kindInvalidInput, err)
	}
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, 0, markError(kindInvalidInput, err)
	}
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	if token := strings.TrimSpace(flags.token); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, markError(kindRuntime, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, response.StatusCode, markError(kindRuntime, err)
	}
	return data, response.StatusCode, nil
}

func serverClientURL(flags *serverClientFlags, path string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(flags.url), "/")
	if base == "" {
		base = "http://127.0.0.1:8080"
	}
	if strings.TrimSpace(flags.socket) != "" {
		base = "http://unix"
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" {
		parsed.Scheme = "http"
	}
	endpoint, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	return parsed.ResolveReference(endpoint).String(), nil
}

func (a app) writeServerResponse(data []byte, jsonReport bool, action string) error {
	if jsonReport {
		_, err := a.stdout.Write(ensureTrailingNewline(data))
		return markError(kindRender, err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		_, writeErr := a.stdout.Write(ensureTrailingNewline(data))
		return markError(kindRender, writeErr)
	}
	id, _ := body["id"].(string)
	status, _ := body["status"].(string)
	if id == "" && status == "" {
		_, err := a.stdout.Write(ensureTrailingNewline(data))
		return markError(kindRender, err)
	}
	fmt.Fprintf(a.stdout, "%s\t%s\t%s\n", action, id, status)
	return nil
}

func (a app) writeServerSubmitAndWait(ctx context.Context, flags *serverClientFlags, submitResponse []byte, interval time.Duration, timeout time.Duration, downloadOutputs bool, outDir string) error {
	var submitted serverJobStatusEnvelope
	if err := json.Unmarshal(submitResponse, &submitted); err != nil {
		return markError(kindRuntime, err)
	}
	_ = json.Unmarshal(submitResponse, &submitted.raw)
	if strings.TrimSpace(submitted.ID) == "" {
		return markError(kindRuntime, fmt.Errorf("server submit response did not include a job id"))
	}
	status, downloads, err := serverWaitForJob(ctx, flags, submitted.ID, interval, timeout, downloadOutputs, outDir)
	if flags.jsonReport {
		report := map[string]any{
			"ok":        err == nil,
			"submitted": serverJobStatusReport(submitted, nil),
			"job":       serverJobStatusReport(status, downloads),
		}
		if err != nil {
			report["error"] = err.Error()
		}
		if writeErr := writeJSON(a.stdout, report); writeErr != nil {
			return markError(kindInternal, writeErr)
		}
		if err != nil {
			return alreadyReported(err)
		}
		return nil
	}
	fmt.Fprintf(a.stdout, "submitted\t%s\t%s\n", submitted.ID, submitted.Status)
	if writeErr := a.writeServerWaitSummary(status, downloads); writeErr != nil {
		return writeErr
	}
	return err
}

func parseJSONLEvents(data []byte) ([]map[string]any, error) {
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	events := []map[string]any{}
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

type serverJobStatusEnvelope struct {
	OK          bool           `json:"ok"`
	ID          string         `json:"id"`
	Status      string         `json:"status"`
	SubmittedAt string         `json:"submitted_at,omitempty"`
	StartedAt   string         `json:"started_at,omitempty"`
	FinishedAt  string         `json:"finished_at,omitempty"`
	Events      []serverEvent  `json:"events,omitempty"`
	Report      *renderReport  `json:"report,omitempty"`
	Error       *errorBody     `json:"error,omitempty"`
	raw         map[string]any `json:"-"`
}

type serverDownloadedOutput struct {
	Source string `json:"source"`
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256,omitempty"`
}

func serverFetchJobStatus(ctx context.Context, flags *serverClientFlags, id string) (serverJobStatusEnvelope, error) {
	response, err := serverClientDo(ctx, flags, http.MethodGet, "/jobs/"+url.PathEscape(id), nil)
	if err != nil {
		return serverJobStatusEnvelope{}, err
	}
	var status serverJobStatusEnvelope
	if err := json.Unmarshal(response, &status); err != nil {
		return serverJobStatusEnvelope{}, markError(kindRuntime, err)
	}
	_ = json.Unmarshal(response, &status.raw)
	return status, nil
}

func serverWaitForJob(ctx context.Context, flags *serverClientFlags, id string, interval time.Duration, timeout time.Duration, downloadOutputs bool, outDir string) (serverJobStatusEnvelope, []serverDownloadedOutput, error) {
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	waitCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	var last serverJobStatusEnvelope
	for {
		status, err := serverFetchJobStatus(waitCtx, flags, id)
		if err != nil {
			return last, nil, err
		}
		last = status
		if serverStatusTerminal(status.Status) {
			var downloads []serverDownloadedOutput
			if downloadOutputs {
				var err error
				downloads, err = serverDownloadJobOutputs(waitCtx, flags, id, status, outDir)
				if err != nil {
					return status, downloads, err
				}
			}
			if status.Status == "succeeded" {
				return status, downloads, nil
			}
			return status, downloads, markError(kindRuntime, fmt.Errorf("server job %s finished with status %s", id, status.Status))
		}
		timer := time.NewTimer(interval)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return last, nil, markError(kindRuntime, fmt.Errorf("timed out waiting for server job %s: %w", id, waitCtx.Err()))
		case <-timer.C:
		}
	}
}

func serverStatusTerminal(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "succeeded", "failed", "canceled":
		return true
	default:
		return false
	}
}

func serverDownloadJobOutputs(ctx context.Context, flags *serverClientFlags, id string, status serverJobStatusEnvelope, outDir string) ([]serverDownloadedOutput, error) {
	if status.Report == nil || len(status.Report.OutputFiles) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(outDir) == "" {
		outDir = "."
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, markError(kindInvalidInput, err)
	}
	downloads := make([]serverDownloadedOutput, 0, len(status.Report.OutputFiles))
	for index, output := range status.Report.OutputFiles {
		data, err := serverClientDo(ctx, flags, http.MethodGet, "/jobs/"+url.PathEscape(id)+"/outputs/"+strconv.Itoa(index), nil)
		if err != nil {
			return downloads, err
		}
		name := filepath.Base(output.Path)
		if name == "." || name == string(filepath.Separator) || name == "" {
			name = fmt.Sprintf("%s-output-%d", id, index)
		}
		target := filepath.Join(outDir, name)
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return downloads, markError(kindRender, err)
		}
		sha, _ := fileSHA256(target)
		downloads = append(downloads, serverDownloadedOutput{Source: output.Path, Path: target, Bytes: int64(len(data)), SHA256: sha})
	}
	return downloads, nil
}

func serverJobStatusReport(status serverJobStatusEnvelope, downloads []serverDownloadedOutput) map[string]any {
	report := status.raw
	if report == nil {
		data, _ := json.Marshal(status)
		_ = json.Unmarshal(data, &report)
	}
	if downloads != nil {
		report["downloaded_outputs"] = downloads
	}
	return report
}

func serverJobLogReport(status serverJobStatusEnvelope) map[string]any {
	report := serverJobStatusReport(status, nil)
	report["duration_ms"] = serverJobDurationMS(status)
	return report
}

func (a app) writeServerWaitSummary(status serverJobStatusEnvelope, downloads []serverDownloadedOutput) error {
	fmt.Fprintf(a.stdout, "%s\t%s\n", status.ID, status.Status)
	for _, download := range downloads {
		fmt.Fprintf(a.stdout, "downloaded\t%s\t%d bytes\n", download.Path, download.Bytes)
	}
	return nil
}

func (a app) writeServerLogs(status serverJobStatusEnvelope) error {
	fmt.Fprintf(a.stdout, "Job %s %s\n", status.ID, status.Status)
	if status.SubmittedAt != "" {
		fmt.Fprintf(a.stdout, "submitted: %s\n", status.SubmittedAt)
	}
	if status.StartedAt != "" {
		fmt.Fprintf(a.stdout, "started:   %s\n", status.StartedAt)
	}
	if status.FinishedAt != "" {
		fmt.Fprintf(a.stdout, "finished:  %s\n", status.FinishedAt)
	}
	if duration := serverJobDurationMS(status); duration >= 0 {
		fmt.Fprintf(a.stdout, "duration:  %dms\n", duration)
	}
	if len(status.Events) > 0 {
		fmt.Fprintln(a.stdout, "events:")
		for _, event := range status.Events {
			message := strings.TrimSpace(event.Message)
			if message == "" {
				fmt.Fprintf(a.stdout, "  %s  %s\n", event.Time, event.Phase)
			} else {
				fmt.Fprintf(a.stdout, "  %s  %-16s %s\n", event.Time, event.Phase, singleLine(message))
			}
		}
	}
	if status.Report != nil && len(status.Report.OutputFiles) > 0 {
		fmt.Fprintln(a.stdout, "outputs:")
		for _, output := range status.Report.OutputFiles {
			fmt.Fprintf(a.stdout, "  %s", output.Path)
			details := []string{}
			if output.Type != "" {
				details = append(details, output.Type)
			}
			if output.Bytes > 0 {
				details = append(details, fmt.Sprintf("%d bytes", output.Bytes))
			}
			if output.Width > 0 && output.Height > 0 {
				details = append(details, fmt.Sprintf("%dx%d", output.Width, output.Height))
			}
			if len(details) > 0 {
				fmt.Fprintf(a.stdout, " (%s)", strings.Join(details, ", "))
			}
			fmt.Fprintln(a.stdout)
		}
	}
	if status.Error != nil {
		fmt.Fprintf(a.stdout, "error: %s", status.Error.Message)
		if status.Error.Code != "" {
			fmt.Fprintf(a.stdout, " (%s)", status.Error.Code)
		}
		fmt.Fprintln(a.stdout)
	}
	return nil
}

func serverJobDurationMS(status serverJobStatusEnvelope) int64 {
	start := status.StartedAt
	if start == "" {
		start = status.SubmittedAt
	}
	if start == "" || status.FinishedAt == "" {
		return -1
	}
	started, err := time.Parse(time.RFC3339Nano, start)
	if err != nil {
		return -1
	}
	finished, err := time.Parse(time.RFC3339Nano, status.FinishedAt)
	if err != nil {
		return -1
	}
	if finished.Before(started) {
		return -1
	}
	return finished.Sub(started).Milliseconds()
}

func ensureTrailingNewline(data []byte) []byte {
	if len(data) == 0 || data[len(data)-1] == '\n' {
		return data
	}
	out := make([]byte, 0, len(data)+1)
	out = append(out, data...)
	out = append(out, '\n')
	return out
}
