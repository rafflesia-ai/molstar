package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type agentDoctorFlags struct {
	jsonReport bool
	outDir     string
	keep       bool
	deep       bool
	timeout    time.Duration
}

type agentDoctorReport struct {
	OK            bool              `json:"ok"`
	Contract      string            `json:"contract"`
	WorkDir       string            `json:"work_dir"`
	ArtifactsKept bool              `json:"artifacts_kept"`
	StartedAt     string            `json:"started_at"`
	DurationMS    int64             `json:"duration_ms"`
	Steps         []agentDoctorStep `json:"steps"`
	Advice        []string          `json:"advice,omitempty"`
}

type agentDoctorStep struct {
	Name       string         `json:"name"`
	OK         bool           `json:"ok"`
	Command    []string       `json:"command,omitempty"`
	AgentCode  string         `json:"agent_code,omitempty"`
	Retryable  bool           `json:"retryable,omitempty"`
	Detail     string         `json:"detail,omitempty"`
	Error      string         `json:"error,omitempty"`
	Report     map[string]any `json:"report,omitempty"`
	Stdout     string         `json:"stdout,omitempty"`
	Stderr     string         `json:"stderr,omitempty"`
	DurationMS int64          `json:"duration_ms"`
}

func (a app) agentCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Run agent-oriented diagnostics and contract checks",
	}
	cmd.AddCommand(a.agentDoctorCommand())
	return cmd
}

func (a app) agentDoctorCommand() *cobra.Command {
	flags := &agentDoctorFlags{timeout: 60 * time.Second}
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check the machine-readable CLI contract an automation agent can rely on",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("agent doctor", flags.jsonReport, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("agent doctor does not accept positional arguments"))
				}
				report, err := a.runAgentDoctor(cmd.Context(), flags)
				if flags.jsonReport {
					if writeErr := writeJSON(a.stdout, report); writeErr != nil {
						return markError(kindInternal, writeErr)
					}
					if err != nil {
						return alreadyReported(err)
					}
					return nil
				}
				status := "passed"
				if !report.OK {
					status = "failed"
				}
				fmt.Fprintf(a.stdout, "agent doctor %s (%d steps, %d ms)\n", status, len(report.Steps), report.DurationMS)
				for _, step := range report.Steps {
					stepStatus := "OK"
					if !step.OK {
						stepStatus = "FAIL"
					}
					fmt.Fprintf(a.stdout, "%-5s %s", stepStatus, step.Name)
					if step.Detail != "" {
						fmt.Fprintf(a.stdout, " (%s)", step.Detail)
					}
					if step.Error != "" {
						fmt.Fprintf(a.stdout, " - %s", singleLine(step.Error))
					}
					fmt.Fprintln(a.stdout)
				}
				for _, advice := range report.Advice {
					fmt.Fprintf(a.stdout, "hint  %s\n", advice)
				}
				return err
			})
		},
	}
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write a machine-readable report to stdout")
	cmd.Flags().StringVar(&flags.outDir, "out-dir", "", "directory for temporary agent doctor artifacts")
	cmd.Flags().BoolVar(&flags.keep, "keep", false, "keep generated agent doctor artifacts")
	cmd.Flags().BoolVar(&flags.deep, "deep", false, "also run the heavier self-test")
	cmd.Flags().DurationVar(&flags.timeout, "timeout", 60*time.Second, "overall agent doctor timeout")
	return cmd
}

func (a app) runAgentDoctor(parent context.Context, flags *agentDoctorFlags) (agentDoctorReport, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(parent, flags.timeout)
	defer cancel()

	workDir := strings.TrimSpace(flags.outDir)
	artifactsKept := flags.keep || workDir != ""
	var err error
	if workDir == "" {
		workDir, err = os.MkdirTemp("", "headlessmolstar-agent-doctor-*")
		if err != nil {
			return agentDoctorReport{}, markError(kindInternal, err)
		}
		if !artifactsKept {
			defer os.RemoveAll(workDir)
		}
	} else {
		workDir, err = filepath.Abs(workDir)
		if err != nil {
			return agentDoctorReport{}, markError(kindInvalidInput, err)
		}
		if err := os.MkdirAll(workDir, 0o755); err != nil {
			return agentDoctorReport{}, markError(kindInvalidInput, err)
		}
	}

	cifPath := filepath.Join(workDir, "agent-doctor.cif")
	if err := os.WriteFile(cifPath, []byte(oneAtomCIF), 0o644); err != nil {
		return agentDoctorReport{}, markError(kindInternal, err)
	}
	jobPath := filepath.Join(workDir, "agent-doctor.job.json")
	j := localSmokeJob(cifPath, filepath.Join(workDir, "agent-doctor.png"))
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return agentDoctorReport{}, markError(kindInternal, err)
	}
	if err := os.WriteFile(jobPath, data, 0o644); err != nil {
		return agentDoctorReport{}, markError(kindInternal, err)
	}

	report := agentDoctorReport{
		OK:            true,
		Contract:      "headlessmolstar.agent-doctor/v1",
		WorkDir:       workDir,
		ArtifactsKept: artifactsKept,
		StartedAt:     start.UTC().Format(time.RFC3339Nano),
	}
	run := func(name string, args ...string) agentDoctorStep {
		stepStart := time.Now()
		stdout, stderr, err := a.runSubcommand(ctx, args...)
		step := agentDoctorStep{
			Name:       name,
			Command:    append([]string{"molstar"}, args...),
			OK:         err == nil,
			Stdout:     strings.TrimSpace(stdout),
			Stderr:     strings.TrimSpace(stderr),
			DurationMS: time.Since(stepStart).Milliseconds(),
		}
		if parsed := parseAgentDoctorJSON(stdout); parsed != nil {
			step.Report = summarizeAgentDoctorStep(name, parsed)
			if ok, present := parsed["ok"].(bool); present {
				step.OK = step.OK && ok
			}
			if errorValue, _ := parsed["error"].(map[string]any); errorValue != nil {
				step.AgentCode, _ = errorValue["agent_code"].(string)
				step.Retryable, _ = errorValue["retryable"].(bool)
				step.Error, _ = errorValue["message"].(string)
			}
		}
		if err != nil && step.Error == "" {
			step.Error = err.Error()
		}
		if step.OK {
			step.Stdout = ""
			step.Stderr = ""
		}
		report.Steps = append(report.Steps, step)
		if !step.OK {
			report.OK = false
		}
		return step
	}

	run("doctor JSON envelope", "doctor", "--skip-probe", "--json")
	run("capabilities contract", "capabilities", "--json")
	run("job schema validation", "job", "validate", jobPath, "--schema", "--json")
	run("job explain contract", "job", "explain", jobPath, "--json")
	run("compile-only inspect", "inspect", jobPath, "--semantic=false", "--json")
	run("render dry-run contract", "render", jobPath, "--dry-run", "--no-log", "--json")
	run("server OpenAPI contract", "serve", "--openapi")
	if flags.deep {
		run("self-test", "self-test", "--json", "--timeout", flags.timeout.String())
	}

	report.DurationMS = time.Since(start).Milliseconds()
	report.Advice = agentDoctorAdvice(report)
	if !report.ArtifactsKept {
		report.WorkDir = ""
	}
	if !report.OK {
		return report, markError(kindDoctor, fmt.Errorf("agent doctor failed"))
	}
	return report, nil
}

func parseAgentDoctorJSON(stdout string) map[string]any {
	stdout = strings.TrimSpace(stdout)
	if stdout == "" {
		return nil
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		return nil
	}
	return parsed
}

func summarizeAgentDoctorStep(name string, parsed map[string]any) map[string]any {
	switch name {
	case "doctor JSON envelope":
		return map[string]any{
			"ok":            parsed["ok"],
			"config_loaded": agentDoctorPathValue(parsed, "config", "loaded"),
			"checks":        agentDoctorPathValue(parsed, "checks", "len"),
			"failed_checks": agentDoctorFailedNames(parsed["checks"]),
		}
	case "capabilities contract":
		return map[string]any{
			"ok":      parsed["ok"],
			"node":    agentDoctorPathValue(parsed, "runtime", "node"),
			"molstar": agentDoctorPathValue(parsed, "runtime", "molstar"),
			"matrix":  agentDoctorCapabilityMatrix(parsed["matrix"]),
		}
	case "job schema validation":
		return map[string]any{"ok": parsed["ok"], "schema": parsed["schema"]}
	case "job explain contract":
		return map[string]any{
			"ok":            parsed["ok"],
			"schema":        parsed["schema"],
			"would_compile": parsed["would_compile"],
			"would_render":  parsed["would_render"],
			"mvs_nodes":     parsed["mvs_nodes"],
			"outputs":       agentDoctorPathValue(parsed, "outputs", "len"),
		}
	case "compile-only inspect":
		return map[string]any{
			"ok":         parsed["ok"],
			"components": agentDoctorPathValue(parsed, "components", "len"),
			"outputs":    agentDoctorPathValue(parsed, "outputs", "len"),
			"semantic":   agentDoctorPathValue(parsed, "semantic", "mode"),
		}
	case "render dry-run contract":
		return map[string]any{
			"ok":                    parsed["ok"],
			"commands":              agentDoctorPathValue(parsed, "commands", "len"),
			"first_command_skipped": agentDoctorPathValue(parsed, "commands", 0, "skipped"),
			"outputs":               agentDoctorPathValue(parsed, "outputs", "len"),
			"renderer_mode":         agentDoctorPathValue(parsed, "diagnostics", "renderer_mode"),
			"job":                   agentDoctorPathValue(parsed, "job", "present"),
			"mvs_document":          agentDoctorPathValue(parsed, "mvs_document", "present"),
		}
	case "server OpenAPI contract":
		return map[string]any{
			"openapi":             parsed["openapi"],
			"paths":               agentDoctorPathValue(parsed, "paths", "len"),
			"has_render":          agentDoctorPathValue(parsed, "paths", "/render", "present"),
			"has_rpc":             agentDoctorPathValue(parsed, "paths", "/rpc", "present"),
			"render_code_samples": agentDoctorPathValue(parsed, "paths", "/render", "post", "x-codeSamples", "len"),
		}
	case "self-test":
		return map[string]any{
			"ok":           parsed["ok"],
			"steps":        agentDoctorPathValue(parsed, "steps", "len"),
			"failed_steps": agentDoctorFailedNames(parsed["steps"]),
		}
	default:
		if errorValue, _ := parsed["error"].(map[string]any); errorValue != nil {
			return map[string]any{"ok": parsed["ok"], "error": errorValue}
		}
		return map[string]any{"ok": parsed["ok"]}
	}
}

func agentDoctorFailedNames(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	var failed []string
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if object["ok"] == true {
			continue
		}
		if name, _ := object["name"].(string); name != "" {
			failed = append(failed, name)
		}
	}
	return failed
}

func agentDoctorCapabilityMatrix(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, map[string]any{
			"target":    object["target"],
			"kind":      object["kind"],
			"available": object["available"],
			"ok":        object["ok"],
			"webgl":     object["webgl"],
			"canvas":    object["canvas"],
		})
	}
	return out
}

func agentDoctorPathValue(value any, path ...any) any {
	current := value
	for i, segment := range path {
		if text, ok := segment.(string); ok {
			if text == "len" {
				switch typed := current.(type) {
				case []any:
					return len(typed)
				case map[string]any:
					return len(typed)
				default:
					return nil
				}
			}
			if text == "present" {
				return current != nil
			}
		}
		switch typed := current.(type) {
		case map[string]any:
			key, ok := segment.(string)
			if !ok {
				return nil
			}
			child, ok := typed[key]
			if !ok {
				return nil
			}
			current = child
		case []any:
			index, ok := segment.(int)
			if !ok || index < 0 || index >= len(typed) {
				return nil
			}
			current = typed[index]
		default:
			return nil
		}
		if i == len(path)-1 {
			return current
		}
	}
	return current
}

func agentDoctorAdvice(report agentDoctorReport) []string {
	advice := []string{}
	for _, step := range report.Steps {
		if step.OK {
			continue
		}
		switch step.AgentCode {
		case "webgl_unavailable", "renderer_unavailable":
			advice = append(advice, "run `molstar doctor --json` and prefer `render --dry-run --json` until renderer dependencies are repaired")
		case "invalid_job":
			advice = append(advice, "run `molstar job validate --schema --json` before render or RPC submission")
		case "network_blocked":
			advice = append(advice, "prime `runtime.cache` first or use `--offline=false` for remote identifiers")
		default:
			advice = append(advice, fmt.Sprintf("inspect the `%s` step output", step.Name))
		}
	}
	return uniqueStrings(advice)
}
