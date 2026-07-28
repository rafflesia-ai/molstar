package render

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type ImageRenderer interface {
	RenderImage(context.Context, ImageRequest) (CommandResult, error)
}

type WorkerPool struct {
	command []string
	stdout  io.Writer
	stderr  io.Writer
	env     []string

	idle            chan *workerProcess
	closed          atomic.Bool
	restarts        atomic.Int64
	restartFailures atomic.Int64
}

type workerProcess struct {
	id      int
	command []string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Scanner
	stderr  io.Writer
	mu      sync.Mutex
	nextID  atomic.Int64
}

type workerRequest struct {
	ID     string         `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params,omitempty"`
}

type workerResponse struct {
	ID     string          `json:"id"`
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *struct {
		Message string `json:"message"`
		Stack   string `json:"stack,omitempty"`
	} `json:"error,omitempty"`
}

type workerRenderResult struct {
	Input    string           `json:"input"`
	Output   string           `json:"output"`
	Size     map[string]int   `json:"size"`
	BadCells []map[string]any `json:"badCells,omitempty"`
}

func NewWorkerPool(size int, command []string, stdout io.Writer, stderr io.Writer, env []string) (*WorkerPool, error) {
	if size < 1 {
		size = 1
	}
	if len(command) == 0 {
		command = defaultWorkerCommand()
	}
	if len(command) == 0 {
		return nil, errors.New("empty worker command")
	}
	pool := &WorkerPool{
		command: command,
		stdout:  outputOrDiscard(stdout),
		stderr:  outputOrDiscard(stderr),
		env:     env,
		idle:    make(chan *workerProcess, size),
	}
	for i := 0; i < size; i++ {
		worker, err := startWorkerProcess(i+1, command, stderr, env)
		if err != nil {
			pool.Close()
			return nil, err
		}
		pool.idle <- worker
	}
	return pool, nil
}

func (p *WorkerPool) RenderImage(ctx context.Context, request ImageRequest) (CommandResult, error) {
	if p.closed.Load() {
		return CommandResult{}, errors.New("renderer worker pool is closed")
	}
	select {
	case worker, ok := <-p.idle:
		if !ok || worker == nil {
			return CommandResult{}, errors.New("renderer worker pool is closed")
		}
		result, err := worker.render(ctx, request)
		if err != nil {
			worker.close()
			replacement, replaceErr := startWorkerProcess(worker.id, p.command, p.stderr, p.env)
			if replaceErr == nil {
				p.restarts.Add(1)
				p.returnWorker(replacement)
			} else {
				p.restartFailures.Add(1)
			}
			return result, err
		}
		p.returnWorker(worker)
		return result, nil
	case <-ctx.Done():
		return CommandResult{}, ctx.Err()
	}
}

func (p *WorkerPool) Capabilities(ctx context.Context) (map[string]any, error) {
	if p.closed.Load() {
		return nil, errors.New("renderer worker pool is closed")
	}
	select {
	case worker, ok := <-p.idle:
		if !ok || worker == nil {
			return nil, errors.New("renderer worker pool is closed")
		}
		result, err := worker.capabilities(ctx)
		if err != nil {
			// A failed/cancelled call may leave a reader goroutine blocked on
			// the worker's stdout scanner; reusing it would race a second
			// reader. Discard and replace the worker, mirroring RenderImage.
			worker.close()
			replacement, replaceErr := startWorkerProcess(worker.id, p.command, p.stderr, p.env)
			if replaceErr == nil {
				p.restarts.Add(1)
				p.returnWorker(replacement)
			} else {
				p.restartFailures.Add(1)
			}
			return result, err
		}
		p.returnWorker(worker)
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *WorkerPool) Stats() map[string]any {
	if p == nil {
		return map[string]any{"enabled": false}
	}
	return map[string]any{
		"closed":           p.closed.Load(),
		"capacity":         cap(p.idle),
		"idle":             len(p.idle),
		"busy":             cap(p.idle) - len(p.idle),
		"restarts":         p.restarts.Load(),
		"restart_failures": p.restartFailures.Load(),
	}
}

func (p *WorkerPool) Close() error {
	if !p.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(p.idle)
	var firstErr error
	for worker := range p.idle {
		if err := worker.shutdown(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (p *WorkerPool) returnWorker(worker *workerProcess) {
	if worker == nil {
		return
	}
	if p.closed.Load() {
		_ = worker.close()
		return
	}
	defer func() {
		if recover() != nil {
			_ = worker.close()
		}
	}()
	p.idle <- worker
}

func startWorkerProcess(id int, command []string, stderr io.Writer, env []string) (*workerProcess, error) {
	args := append([]string{}, command...)
	if !containsArg(args, "--worker") {
		args = append(args, "--worker")
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = commandEnv(env)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		_, _ = io.Copy(outputOrDiscard(stderr), stderrPipe)
	}()
	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	return &workerProcess{
		id:      id,
		command: args,
		cmd:     cmd,
		stdin:   stdin,
		stdout:  scanner,
		stderr:  outputOrDiscard(stderr),
	}, nil
}

func (w *workerProcess) render(ctx context.Context, request ImageRequest) (CommandResult, error) {
	width, height := request.Output.SizeOrDefault(800, 800)
	if err := os.MkdirAll(filepathDir(request.Output.Path), 0o755); err != nil {
		return CommandResult{}, err
	}
	params := map[string]any{
		"input":       request.InputMVS,
		"output":      request.Output.Path,
		"width":       width,
		"height":      height,
		"molj":        request.SaveMolj,
		"transparent": request.Output.Transparent,
	}
	start := time.Now()
	result := CommandResult{
		Command:   w.command,
		StartedAt: start.UTC().Format(time.RFC3339Nano),
		Worker:    true,
		WorkerID:  w.id,
	}
	var payload workerRenderResult
	if err := w.call(ctx, "render", params, &payload); err != nil {
		result.DurationMS = time.Since(start).Milliseconds()
		result.ExitCode = -1
		result.Stderr = truncateForReport(err.Error())
		return result, err
	}
	result.DurationMS = time.Since(start).Milliseconds()
	result.ExitCode = 0
	result.BadCells = payload.BadCells
	return result, nil
}

func (w *workerProcess) capabilities(ctx context.Context) (map[string]any, error) {
	var payload map[string]any
	if err := w.call(ctx, "capabilities", nil, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (w *workerProcess) call(ctx context.Context, method string, params map[string]any, out any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	id := fmt.Sprintf("req_%d", w.nextID.Add(1))
	request := workerRequest{ID: id, Method: method, Params: params}
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	if _, err := w.stdin.Write(append(data, '\n')); err != nil {
		return err
	}
	responseCh := make(chan workerResponse, 1)
	errCh := make(chan error, 1)
	go func() {
		for w.stdout.Scan() {
			var response workerResponse
			if err := json.Unmarshal(w.stdout.Bytes(), &response); err != nil {
				errCh <- err
				return
			}
			if response.ID == id || response.ID == "" {
				responseCh <- response
				return
			}
		}
		if err := w.stdout.Err(); err != nil {
			errCh <- err
			return
		}
		// The worker closed its stdout without answering, which means the
		// process died mid-request. A bare io.EOF here reached callers as the
		// single word "EOF", telling an operator nothing about what happened.
		errCh <- fmt.Errorf("renderer worker exited before answering %s: %w", method, io.EOF)
	}()
	select {
	case response := <-responseCh:
		if !response.OK {
			if response.Error != nil {
				return errors.New(response.Error.Message)
			}
			return errors.New("worker request failed")
		}
		if out != nil && len(response.Result) > 0 {
			return json.Unmarshal(response.Result, out)
		}
		return nil
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *workerProcess) shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = w.call(ctx, "shutdown", nil, nil)
	return w.close()
}

func (w *workerProcess) close() error {
	_ = w.stdin.Close()
	if w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
	}
	return w.cmd.Wait()
}

func defaultWorkerCommand() []string {
	if localNode, renderer := findLocalScript("scripts", "render-mvs.js"); localNode != "" && renderer != "" {
		return []string{localNode, renderer}
	}
	renderer, _ := defaultRendererCommand()
	return renderer
}

func containsArg(args []string, target string) bool {
	for _, arg := range args {
		if arg == target {
			return true
		}
	}
	return false
}

func filepathDir(path string) string {
	dir := filepath.Dir(path)
	if dir == "" {
		return "."
	}
	return dir
}
