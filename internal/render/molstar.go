package render

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rafflesia-ai/molstar/internal/job"
)

type Molstar struct {
	RendererCommand         []string
	RendererFallbackCommand []string
	ValidateCommand         []string
	WorkingDirectory        string
	Stdout                  io.Writer
	Stderr                  io.Writer
	DryRun                  bool
	Quiet                   bool
	Env                     []string
}

type ImageRequest struct {
	InputMVS string
	Output   job.Output
	SaveMolj bool
}

type CommandResult struct {
	Command        []string         `json:"command"`
	Skipped        bool             `json:"skipped"`
	FallbackOf     []string         `json:"fallback_of,omitempty"`
	FallbackReason string           `json:"fallback_reason,omitempty"`
	StartedAt      string           `json:"started_at,omitempty"`
	DurationMS     int64            `json:"duration_ms,omitempty"`
	ExitCode       int              `json:"exit_code,omitempty"`
	Stdout         string           `json:"stdout,omitempty"`
	Stderr         string           `json:"stderr,omitempty"`
	Worker         bool             `json:"worker,omitempty"`
	WorkerID       int              `json:"worker_id,omitempty"`
	BadCells       []map[string]any `json:"bad_cells,omitempty"`

	// RawStdout is the renderer's complete stdout. Stdout is truncated so a
	// runaway process cannot bloat a report, which corrupts any structured
	// payload larger than the limit; parse this instead. Never serialized.
	RawStdout string `json:"-"`
}

func NewMolstar() Molstar {
	renderer, fallback := defaultRendererCommand()
	return Molstar{
		RendererCommand:         renderer,
		RendererFallbackCommand: fallback,
		ValidateCommand:         defaultValidateCommand(),
		Stdout:                  os.Stdout,
		Stderr:                  os.Stderr,
	}
}

func (m Molstar) RenderImage(ctx context.Context, request ImageRequest) (CommandResult, error) {
	args := m.renderImageArgs(m.RendererCommand, request)
	if err := os.MkdirAll(filepath.Dir(request.Output.Path), 0o755); err != nil {
		return CommandResult{}, err
	}
	result, err := m.runImageCommand(ctx, m.RendererCommand, args)
	if err == nil || len(m.RendererFallbackCommand) == 0 || sameCommand(m.RendererCommand, m.RendererFallbackCommand) {
		return result, err
	}
	fmt.Fprintf(outputOrDiscard(m.Stderr), "renderer warning: primary renderer failed (%s); retrying with fallback\n", err)
	fallbackArgs := m.renderImageArgs(m.RendererFallbackCommand, request)
	fallbackResult, fallbackErr := m.runImageCommand(ctx, m.RendererFallbackCommand, fallbackArgs)
	fallbackResult.FallbackOf = args
	fallbackResult.FallbackReason = err.Error()
	if fallbackErr != nil {
		return fallbackResult, fmt.Errorf("primary renderer failed: %w; fallback renderer failed: %v", err, fallbackErr)
	}
	return fallbackResult, nil
}

func (m Molstar) InspectMVS(ctx context.Context, path string) (CommandResult, map[string]any, error) {
	args := append([]string{}, m.RendererCommand...)
	args = append(args, "--inspect", "-i", path, "--json", "--quiet")
	result, err := m.run(ctx, args)
	if err != nil {
		return result, nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(result.stdoutForParsing()), &payload); err != nil {
		return result, nil, fmt.Errorf("decode Mol* inspect JSON: %w", err)
	}
	return result, payload, nil
}

func (m Molstar) runImageCommand(ctx context.Context, command []string, args []string) (CommandResult, error) {
	if !m.Quiet || commandSupportsQuiet(command) {
		return m.run(ctx, args)
	}
	quiet := m
	quiet.Stdout = nil
	quiet.Stderr = nil
	return quiet.run(ctx, args)
}

func (m Molstar) renderImageArgs(command []string, request ImageRequest) []string {
	width, height := request.Output.SizeOrDefault(800, 800)
	args := append([]string{}, command...)
	args = append(args,
		"-i", request.InputMVS,
		"-o", request.Output.Path,
		"--size", fmt.Sprintf("%dx%d", width, height),
	)
	if request.SaveMolj {
		args = append(args, "--molj")
	}
	if request.Output.Transparent {
		args = append(args, "--transparent")
	}
	if m.Quiet && commandSupportsQuiet(command) {
		args = append(args, "--quiet")
	}
	return args
}

func (m Molstar) ValidateMVS(ctx context.Context, path string, noExtra bool) (CommandResult, error) {
	args := append([]string{}, m.ValidateCommand...)
	if noExtra {
		args = append(args, "--no-extra")
	}
	args = append(args, path)
	return m.run(ctx, args)
}

func (m Molstar) run(ctx context.Context, args []string) (CommandResult, error) {
	if len(args) == 0 {
		return CommandResult{}, errors.New("empty command")
	}
	start := time.Now()
	if m.DryRun {
		fmt.Fprintln(outputOrDiscard(m.Stderr), strings.Join(args, " "))
		return CommandResult{Command: args, Skipped: true, StartedAt: start.UTC().Format(time.RFC3339Nano)}, nil
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = m.WorkingDirectory
	cmd.Env = commandEnv(m.Env)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = io.MultiWriter(&limitedWriter{w: outputOrDiscard(m.Stdout), remaining: liveStreamLimit}, &stdout)
	cmd.Stderr = io.MultiWriter(&limitedWriter{w: outputOrDiscard(m.Stderr), remaining: liveStreamLimit}, &stderr)
	if err := runCommandWithContext(ctx, cmd); err != nil {
		result := CommandResult{
			Command:    args,
			StartedAt:  start.UTC().Format(time.RFC3339Nano),
			DurationMS: time.Since(start).Milliseconds(),
			ExitCode:   exitCode(err),
			Stdout:     truncateForReport(stdout.String()),
			Stderr:     truncateForReport(stderr.String()),
			RawStdout:  stdout.String(),
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return result, ctxErr
		}
		return result, err
	}
	return CommandResult{
		Command:    args,
		StartedAt:  start.UTC().Format(time.RFC3339Nano),
		DurationMS: time.Since(start).Milliseconds(),
		ExitCode:   0,
		Stdout:     truncateForReport(stdout.String()),
		Stderr:     truncateForReport(stderr.String()),
		RawStdout:  stdout.String(),
	}, nil
}

// stdoutForParsing returns the renderer's complete stdout, falling back to the
// report-truncated copy for results that did not come from run (dry runs, tests,
// and results decoded from a run log).
func (r CommandResult) stdoutForParsing() string {
	if r.RawStdout != "" {
		return r.RawStdout
	}
	return r.Stdout
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

func truncateForReport(value string) string {
	const limit = 16 << 10
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "\n[truncated]"
}

const liveStreamLimit = 16 << 10

// limitedWriter forwards at most remaining bytes to the underlying writer, then
// drops the rest after emitting a single truncation notice. It caps the live
// renderer stderr so a crashing Node process — which can dump an entire
// minified module to stderr — cannot flood the terminal. The meaningful error
// is emitted first and is preserved; only the runaway tail is dropped. The full
// output is still captured separately for the JSON report.
type limitedWriter struct {
	w         io.Writer
	remaining int
	truncated bool
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.remaining > 0 {
		n := len(p)
		if n > l.remaining {
			n = l.remaining
		}
		if _, err := l.w.Write(p[:n]); err != nil {
			return len(p), nil
		}
		l.remaining -= n
	}
	if l.remaining <= 0 && !l.truncated {
		l.truncated = true
		_, _ = io.WriteString(l.w, "\n[renderer output truncated]\n")
	}
	return len(p), nil
}

func Available(command []string) bool {
	if len(command) == 0 {
		return false
	}
	if strings.Contains(command[0], string(os.PathSeparator)) {
		info, err := os.Stat(command[0])
		if err != nil || info.IsDir() {
			return false
		}
	} else if _, err := exec.LookPath(command[0]); err != nil {
		return false
	}
	for _, arg := range command[1:] {
		if strings.Contains(arg, string(os.PathSeparator)) {
			info, err := os.Stat(arg)
			if err != nil || info.IsDir() {
				return false
			}
		}
	}
	return true
}

func splitCommand(command string) []string {
	return strings.Fields(command)
}

func defaultRendererCommand() ([]string, []string) {
	if value := strings.TrimSpace(os.Getenv("MOLSTAR_RENDER")); value != "" {
		return splitCommand(value), splitCommand(strings.TrimSpace(os.Getenv("MOLSTAR_RENDER_FALLBACK")))
	}
	if config, _, err := LoadRuntimeConfig(); err == nil {
		primary := validCommand(config.RendererCommand)
		fallback := validCommand(config.RendererFallbackCommand)
		if len(primary) > 0 {
			return primary, fallback
		}
	}
	if localNode, renderer := findLocalScript("scripts", "render-mvs.js"); localNode != "" && renderer != "" {
		fallbackBin, fallbackNode, wrapper := findLocalNodeBin("mvs-render")
		var fallback []string
		if fallbackBin != "" && fallbackNode != "" && wrapper != "" {
			fallback = []string{fallbackNode, wrapper, fallbackBin}
		}
		return []string{localNode, renderer}, fallback
	}
	fallback := defaultCommand("MOLSTAR_RENDER_FALLBACK", "mvs-render")
	return fallback, nil
}

func defaultValidateCommand() []string {
	if value := strings.TrimSpace(os.Getenv("MOLSTAR_VALIDATE")); value != "" {
		return splitCommand(value)
	}
	if config, _, err := LoadRuntimeConfig(); err == nil {
		if command := validCommand(config.ValidateCommand); len(command) > 0 {
			return command
		}
	}
	return defaultCommand("MOLSTAR_VALIDATE", "mvs-validate")
}

func defaultCommand(envKey, bin string) []string {
	if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
		return splitCommand(value)
	}
	if localBin, localNode, wrapper := findLocalNodeBin(bin); localBin != "" {
		if localNode != "" && wrapper != "" {
			return []string{localNode, wrapper, localBin}
		}
		if localNode != "" {
			return []string{localNode, localBin}
		}
		return []string{localBin}
	}
	if _, err := exec.LookPath(bin); err == nil {
		return []string{bin}
	}
	return []string{filepath.Join("node_modules", ".bin", bin)}
}

var (
	nodeRunnableMu    sync.Mutex
	nodeRunnableCache = map[string]bool{}
)

// nodeRunnable reports whether the node interpreter at path can actually
// execute. A file passing os.Stat is not enough: the bundled node ships from
// the platform-specific "node" npm package and may be the wrong architecture
// (or a truncated/corrupt file) while still existing on disk. The result is
// cached per path so command resolution stays cheap across repeated renders.
func nodeRunnable(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	nodeRunnableMu.Lock()
	if cached, ok := nodeRunnableCache[path]; ok {
		nodeRunnableMu.Unlock()
		return cached
	}
	nodeRunnableMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runnable := exec.CommandContext(ctx, path, "--version").Run() == nil

	nodeRunnableMu.Lock()
	nodeRunnableCache[path] = runnable
	nodeRunnableMu.Unlock()
	return runnable
}

// pickNode returns a runnable node interpreter. It prefers the bundled node at
// bundledPath, but falls back to a `node` on PATH when the bundled binary is
// missing or cannot execute. Returns "" when no runnable node is found.
func pickNode(bundledPath string) string {
	if nodeRunnable(bundledPath) {
		return bundledPath
	}
	if sys, err := exec.LookPath("node"); err == nil && nodeRunnable(sys) {
		return sys
	}
	return ""
}

func findLocalScript(parts ...string) (string, string) {
	dir, err := os.Getwd()
	if err != nil {
		return "", ""
	}
	for {
		scriptParts := append([]string{dir}, parts...)
		script := filepath.Join(scriptParts...)
		if scriptInfo, scriptErr := os.Stat(script); scriptErr == nil && !scriptInfo.IsDir() {
			bundledNode := filepath.Join(dir, "node_modules", "node", "bin", "node")
			if node := pickNode(bundledNode); node != "" {
				return node, script
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ""
		}
		dir = parent
	}
}

func findLocalNodeBin(bin string) (string, string, string) {
	dir, err := os.Getwd()
	if err != nil {
		return "", "", ""
	}
	for {
		candidate := filepath.Join(dir, "node_modules", ".bin", bin)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			bundledNode := filepath.Join(dir, "node_modules", "node", "bin", "node")
			wrapper := filepath.Join(dir, "scripts", "molstar-node-cli.js")
			if wrapperInfo, err := os.Stat(wrapper); err != nil || wrapperInfo.IsDir() {
				wrapper = ""
			}
			if node := pickNode(bundledNode); node != "" {
				return candidate, node, wrapper
			}
			return candidate, "", wrapper
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", ""
		}
		dir = parent
	}
}

func commandEnv(extra []string) []string {
	env := append([]string{}, os.Environ()...)
	if local := findLocalBinDir(); local != "" {
		env = prependPath(env, local)
	}
	env = append(env, extra...)
	return env
}

func findLocalBinDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		candidate := filepath.Join(dir, "node_modules", ".bin")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func prependPath(env []string, path string) []string {
	for i, value := range env {
		if strings.HasPrefix(value, "PATH=") {
			env[i] = "PATH=" + path + string(os.PathListSeparator) + strings.TrimPrefix(value, "PATH=")
			return env
		}
	}
	return append(env, "PATH="+path)
}

func commandSupportsQuiet(command []string) bool {
	return CommandKind(command) == "owned-renderer"
}

func SupportsCapabilities(command []string) bool {
	return CommandKind(command) == "owned-renderer"
}

func SupportsWorker(command []string) bool {
	return CommandKind(command) == "owned-renderer"
}

func CommandKind(command []string) string {
	for _, part := range command {
		if filepath.Base(part) == "render-mvs.js" {
			return "owned-renderer"
		}
		if filepath.Base(part) == "mvs-render" {
			return "molstar-mvs-render"
		}
		if filepath.Base(part) == "mvs-validate" {
			return "molstar-mvs-validate"
		}
	}
	if len(command) == 0 {
		return "unconfigured"
	}
	return "external"
}

func outputOrDiscard(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}

func sameCommand(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
