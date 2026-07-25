package render

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

type CapabilitiesReport struct {
	OK        bool           `json:"ok"`
	Command   CommandResult  `json:"command"`
	Renderer  map[string]any `json:"renderer,omitempty"`
	Error     string         `json:"error,omitempty"`
	StartedAt string         `json:"started_at,omitempty"`
}

func (m Molstar) Capabilities(ctx context.Context) CapabilitiesReport {
	start := time.Now()
	args := append([]string{}, m.RendererCommand...)
	args = append(args, "--capabilities")
	result := CommandResult{Command: args, StartedAt: start.UTC().Format(time.RFC3339Nano)}
	if len(args) == 0 {
		return CapabilitiesReport{OK: false, Command: result, Error: "empty renderer command", StartedAt: result.StartedAt}
	}
	if m.DryRun {
		result.Skipped = true
		return CapabilitiesReport{OK: true, Command: result, Renderer: map[string]any{"dry_run": true}, StartedAt: result.StartedAt}
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = commandEnv(m.Env)
	output, err := cmd.Output()
	result.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		result.ExitCode = exitCode(err)
		if exit, ok := err.(*exec.ExitError); ok {
			result.Stderr = truncateForReport(string(exit.Stderr))
		}
		return CapabilitiesReport{OK: false, Command: result, Error: err.Error(), StartedAt: result.StartedAt}
	}
	result.ExitCode = 0
	result.Stdout = truncateForReport(string(output))
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		return CapabilitiesReport{OK: false, Command: result, Error: fmt.Sprintf("decode capabilities JSON: %v", err), StartedAt: result.StartedAt}
	}
	return CapabilitiesReport{OK: true, Command: result, Renderer: decoded, StartedAt: result.StartedAt}
}
