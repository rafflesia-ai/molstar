package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rafflesia-ai/molstar/internal/mvs"
)

type ciArtifactReport struct {
	OK        bool      `json:"ok"`
	Input     string    `json:"input"`
	Error     errorBody `json:"error"`
	CreatedAt string    `json:"created_at"`
	Files     []string  `json:"files"`
}

func (a app) writeCIArtifact(ctx context.Context, dir string, input string, report renderReport, renderErr error) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	written := []string{}
	add := func(path string, write func(string) error) {
		target := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return
		}
		if err := write(target); err == nil {
			written = append(written, target)
		}
	}
	add("render-report.json", func(path string) error { return writeJSONFile(path, report) })
	if report.Job != nil {
		add("job.json", func(path string) error { return writeJSONFile(path, report.Job) })
	}
	if report.MVSDocument != nil {
		add("scene.mvsj", func(path string) error { return mvs.WriteFile(path, *report.MVSDocument) })
	}
	explain := map[string]any{"ok": false, "input": input, "error": renderErr.Error()}
	if report.Job != nil {
		explain["runtime"] = report.Job.Runtime
		explain["inputs"] = report.Job.Inputs
		explain["outputs"] = report.Job.Outputs
		explain["scene"] = report.Job.Scene
	}
	add("explain.json", func(path string) error { return writeJSONFile(path, explain) })
	doctorOut, _, doctorErr := a.runSubcommand(ctx, "doctor", "--skip-probe", "--json")
	if doctorErr == nil {
		add("doctor.json", func(path string) error { return os.WriteFile(path, []byte(doctorOut), 0o644) })
	} else {
		add("doctor.json", func(path string) error {
			return writeJSONFile(path, errorReport{OK: false, Command: "doctor", Error: newErrorBody(doctorErr)})
		})
	}
	summary := ciArtifactReport{
		OK:        false,
		Input:     input,
		Error:     newErrorBody(renderErr),
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Files:     written,
	}
	if err := writeJSONFile(filepath.Join(dir, "ci-artifact.json"), summary); err != nil {
		return err
	}
	fmt.Fprintf(a.stderr, "wrote CI artifact %s\n", dir)
	return nil
}
