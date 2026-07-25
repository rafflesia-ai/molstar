package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/sacha-ichbiah/molstar/internal/render"
)

type workerRendererSelection struct {
	Pool          *render.WorkerPool
	Mode          string
	Close         func() error
	FallbackError error
}

func selectWorkerRenderer(mode string, workerCommand string, poolSize int, runner render.Molstar, stderr io.Writer, dryRun bool) (workerRendererSelection, error) {
	if strings.TrimSpace(mode) == "" {
		mode = "subprocess"
	}
	switch mode {
	case "subprocess":
		return workerRendererSelection{Mode: "subprocess"}, nil
	case "worker", "auto":
	default:
		return workerRendererSelection{}, fmt.Errorf("unsupported renderer mode %q; expected subprocess, worker, or auto", mode)
	}
	if dryRun {
		if mode == "worker" {
			return workerRendererSelection{}, fmt.Errorf("--renderer-mode worker cannot be combined with --dry-run")
		}
		return workerRendererSelection{Mode: "subprocess", FallbackError: fmt.Errorf("worker mode disabled for dry-run")}, nil
	}
	command := runner.RendererCommand
	if strings.TrimSpace(workerCommand) != "" {
		command = strings.Fields(workerCommand)
	}
	pool, err := render.NewWorkerPool(poolSize, command, nil, stderr, nil)
	if err != nil {
		if mode == "auto" {
			return workerRendererSelection{Mode: "subprocess", FallbackError: err}, nil
		}
		return workerRendererSelection{}, err
	}
	return workerRendererSelection{
		Pool:  pool,
		Mode:  "worker",
		Close: pool.Close,
	}, nil
}
