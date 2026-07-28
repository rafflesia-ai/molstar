package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type app struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	args   []string
}

func Execute(ctx context.Context, args []string) error {
	a := app{stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr, args: args}
	root := a.rootCommand()
	root.SetArgs(args)
	root.SetOut(a.stdout)
	root.SetErr(a.stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		var reported reportedError
		if errors.As(err, &reported) {
			return err
		}
		err = markUsageError(err)
		if wantsJSONOnError(args) {
			_ = a.writeJSONError(commandNameForArgs(root, args), err)
			return alreadyReported(err)
		}
		fmt.Fprintln(a.stderr, err)
		return err
	}
	return nil
}

func (a app) rootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "molstar",
		Short:         "Headless Mol* job runner",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	// Inherited by every subcommand: a bad flag is caller input, not an internal fault.
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return markError(kindInvalidInput, err)
	})
	cmd.AddCommand(a.renderCommand())
	cmd.AddCommand(a.agentCommand())
	cmd.AddCommand(a.quickstartCommand())
	cmd.AddCommand(a.versionCommand())
	cmd.AddCommand(a.sceneCommand())
	cmd.AddCommand(a.batchCommand())
	cmd.AddCommand(a.benchCommand())
	cmd.AddCommand(a.diagnoseCommand())
	cmd.AddCommand(a.doctorCommand())
	cmd.AddCommand(a.capabilitiesCommand())
	cmd.AddCommand(a.selfTestCommand())
	cmd.AddCommand(a.jobCommand())
	cmd.AddCommand(a.jobsCommand())
	cmd.AddCommand(a.recipeCommand())
	cmd.AddCommand(a.inspectCommand())
	cmd.AddCommand(a.cacheCommand())
	cmd.AddCommand(a.compatCommand())
	cmd.AddCommand(a.presetsCommand())
	cmd.AddCommand(a.selectorsCommand())
	cmd.AddCommand(a.examplesCommand())
	cmd.AddCommand(a.fixturesCommand())
	cmd.AddCommand(a.logsCommand())
	cmd.AddCommand(a.smokeCommand())
	cmd.AddCommand(a.serveCommand())
	cmd.AddCommand(a.serverCommand())
	cmd.AddCommand(a.rpcCommand())
	cmd.AddCommand(a.completionCommand())
	cmd.AddCommand(a.docsCommand())
	cmd.AddCommand(a.installLocalCommand())
	cmd.AddCommand(a.installArtifactCommand())
	cmd.AddCommand(a.updateRuntimeCommand())
	return cmd
}

func commandNameForArgs(root *cobra.Command, args []string) string {
	command, _, err := root.Find(args)
	if err != nil || command == nil {
		return "molstar"
	}
	path := command.CommandPath()
	rootPath := root.CommandPath()
	if path == rootPath {
		return rootPath
	}
	return strings.TrimPrefix(path, rootPath+" ")
}

type errorKind string

const (
	kindInternal     errorKind = "internal_error"
	kindInvalidInput errorKind = "invalid_input"
	kindInvalidScene errorKind = "invalid_scene"
	kindValidation   errorKind = "validation_failed"
	kindRuntime      errorKind = "runtime_blocked"
	kindRender       errorKind = "render_failed"
	kindRenderer     errorKind = "renderer_unavailable"
	kindRendererABI  errorKind = "renderer_abi_mismatch"
	kindNetwork      errorKind = "network_error"
	kindSecurity     errorKind = "security_policy"
	kindServerBusy   errorKind = "server_busy"
	kindDoctor       errorKind = "doctor_failed"
	kindCanceled     errorKind = "canceled"
)

type kindError struct {
	kind errorKind
	err  error
}

func (e kindError) Error() string {
	return e.err.Error()
}

func (e kindError) Unwrap() error {
	return e.err
}

func markError(kind errorKind, err error) error {
	if err == nil {
		return nil
	}
	var existing kindError
	if errors.As(err, &existing) {
		return err
	}
	return kindError{kind: kind, err: err}
}

type reportedError struct {
	err error
}

func (e reportedError) Error() string {
	return e.err.Error()
}

func (e reportedError) Unwrap() error {
	return e.err
}

func alreadyReported(err error) error {
	return reportedError{err: err}
}

type errorReport struct {
	OK        bool      `json:"ok"`
	Command   string    `json:"command"`
	Error     errorBody `json:"error"`
	Timestamp string    `json:"timestamp"`
}

type errorBody struct {
	Code      string   `json:"code"`
	AgentCode string   `json:"agent_code"`
	Message   string   `json:"message"`
	Retryable bool     `json:"retryable"`
	ExitCode  int      `json:"exit_code"`
	Diagnosis []string `json:"diagnosis"`
}

func (a app) runWithJSONErrors(command string, jsonReport bool, run func() error) error {
	err := run()
	if err == nil {
		return nil
	}
	var reported reportedError
	if errors.As(err, &reported) {
		return err
	}
	if jsonReport {
		_ = a.writeJSONError(command, err)
		return alreadyReported(err)
	}
	return err
}

func (a app) writeJSONError(command string, err error) error {
	report := errorReport{
		OK:        false,
		Command:   command,
		Error:     newErrorBody(err),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	return writeJSON(a.stdout, report)
}

func newErrorBody(err error) errorBody {
	body := errorBody{
		Code:      string(classifyError(err)),
		AgentCode: string(agentErrorCode(err)),
		Message:   err.Error(),
		Retryable: errorRetryable(err),
		ExitCode:  ExitCode(err),
		Diagnosis: diagnoseError(err),
	}
	if body.ExitCode == 0 {
		body.ExitCode = 1
	}
	if body.Diagnosis == nil {
		body.Diagnosis = []string{}
	}
	return body
}

func agentErrorCode(err error) errorKind {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "headless webgl context is unavailable") ||
		strings.Contains(message, "gl returned null") ||
		strings.Contains(message, "reading 'getextension'") ||
		strings.Contains(message, "reading \"getextension\"") {
		return "webgl_unavailable"
	}
	switch classifyError(err) {
	case kindInvalidInput, kindInvalidScene, kindValidation:
		return "invalid_job"
	case kindRenderer, kindRendererABI, kindRender:
		return kindRenderer
	case kindNetwork:
		return "network_blocked"
	case kindSecurity, kindRuntime:
		return kindSecurity
	case kindServerBusy:
		return kindServerBusy
	case kindCanceled:
		return kindCanceled
	default:
		return kindInternal
	}
}

func errorRetryable(err error) bool {
	switch agentErrorCode(err) {
	case kindServerBusy, "renderer_unavailable", "webgl_unavailable":
		return true
	case "network_blocked":
		// A transport failure may clear on its own; a deliberately disabled
		// network with an empty cache never will, so retrying it is pure waste.
		return !deliberatelyOffline(err)
	default:
		return false
	}
}

func deliberatelyOffline(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "offline") ||
		strings.Contains(message, "network is disabled") ||
		strings.Contains(message, "network=false")
}

// usageErrorPrefixes are the messages Cobra produces when the caller typed a bad
// command line. They are caller-input problems, so they must classify as
// invalid_input rather than falling through to internal_error.
var usageErrorPrefixes = []string{
	"unknown command",
	"unknown flag",
	"unknown shorthand flag",
	"flag needs an argument",
	"invalid argument",
	"bad flag syntax",
}

// markPrepareError types a runtime-preparation failure. Preparation covers
// policy checks, cache lookups, and remote downloads, so blanket-marking every
// failure as kindRuntime reported network outages as security_policy. Let the
// message pick a more specific kind when it recognizes one.
func markPrepareError(err error) error {
	if err == nil {
		return nil
	}
	var typed kindError
	if errors.As(err, &typed) {
		return err
	}
	if kind := classifyError(err); kind != kindInternal {
		return markError(kind, err)
	}
	return markError(kindRuntime, err)
}

func markUsageError(err error) error {
	if err == nil {
		return nil
	}
	var typed kindError
	if errors.As(err, &typed) {
		return err
	}
	message := strings.ToLower(err.Error())
	for _, prefix := range usageErrorPrefixes {
		if strings.HasPrefix(message, prefix) {
			return markError(kindInvalidInput, err)
		}
	}
	return err
}

func classifyError(err error) errorKind {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return kindCanceled
	}
	var typed kindError
	if errors.As(err, &typed) {
		return typed.kind
	}
	message := strings.ToLower(err.Error())
	switch {
	// Ahead of the network rules: a damaged cache entry is bad input, not a
	// transport problem, and re-fetching it is a deliberate act rather than
	// something a retry loop should attempt.
	case strings.Contains(message, "is corrupt"):
		return kindInvalidInput
	case strings.Contains(message, "appears blank"):
		// The renderer ran and produced a well-formed image; the scene just had
		// nothing visible in it.
		return kindInvalidScene
	case strings.Contains(message, "node_module_version") ||
		strings.Contains(message, "err_dlopen_failed") ||
		strings.Contains(message, "module did not self-register") ||
		strings.Contains(message, "compiled against a different node.js version") ||
		strings.Contains(message, "invalid elf") ||
		strings.Contains(message, "wrong architecture"):
		return kindRendererABI
	case strings.Contains(message, "cannot find module") ||
		strings.Contains(message, "executable file not found") ||
		strings.Contains(message, "command is not available") ||
		strings.Contains(message, "empty worker command") ||
		// A worker that dies mid-request is a transient renderer fault: the pool
		// respawns, so resubmitting the same job usually succeeds.
		strings.Contains(message, "renderer worker exited") ||
		strings.Contains(message, "headless webgl context is unavailable") ||
		strings.Contains(message, "gl returned null") ||
		strings.Contains(message, "reading 'getextension'") ||
		strings.Contains(message, "reading \"getextension\""):
		return kindRenderer
	case strings.Contains(message, "render worker queue is full") ||
		strings.Contains(message, "server_busy") ||
		strings.Contains(message, "server busy"):
		return kindServerBusy
	case strings.Contains(message, "allow_hosts") ||
		strings.Contains(message, "allow_paths") ||
		strings.Contains(message, "not allowed") ||
		strings.Contains(message, "outside allowed"):
		return kindSecurity
	case strings.Contains(message, "offline") ||
		strings.Contains(message, "network") ||
		strings.Contains(message, "download") ||
		strings.Contains(message, "no cached"):
		return kindNetwork
	// Transport failures from the Go HTTP client. Without these a DNS or
	// connection failure — the canonical transient, retryable error — fell
	// through to internal_error, or to security_policy once the runtime wrapper
	// had typed it, and callers were told not to retry a fetch that had simply
	// found the network down.
	case strings.Contains(message, "no such host") ||
		strings.Contains(message, "dial tcp") ||
		strings.Contains(message, "connection refused") ||
		strings.Contains(message, "connection reset") ||
		strings.Contains(message, "i/o timeout") ||
		strings.Contains(message, "tls handshake") ||
		strings.Contains(message, "server misbehaving") ||
		strings.Contains(message, "http status"):
		return kindNetwork
	}
	return kindInternal
}

func diagnoseError(err error) []string {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	var hints []string
	switch {
	// Ahead of the network hint: every cache path contains a "downloads"
	// directory, so a message naming one would otherwise be diagnosed as a
	// network problem.
	case strings.Contains(message, "is corrupt"):
		hints = append(hints, "remove the damaged entry with `molstar cache prune`, then re-run with network access so it can be fetched again")
	case strings.Contains(message, "appears blank"):
		hints = append(hints, "the renderer worked but the scene had nothing visible; check that the component selectors match atoms with `molstar inspect JOB --semantic=auto --json`, and check camera focus/zoom")
	case strings.Contains(message, "unsupported format"):
		hints = append(hints, "check the input file extension or pass an explicit format")
	case strings.Contains(message, "headless webgl context is unavailable") ||
		strings.Contains(message, "gl returned null") ||
		strings.Contains(message, "reading 'getextension'") ||
		strings.Contains(message, "reading \"getextension\""):
		hints = append(hints, "the renderer could not create a WebGL context; in Docker use the software GL profile or run `npm run docker:verify:render` to validate rendering")
	case classifyError(err) == kindRendererABI:
		hints = append(hints, "reinstall renderer dependencies with the active Node version, then run `molstar doctor --json`")
	case classifyError(err) == kindRenderer:
		hints = append(hints, "run `molstar doctor --fix` to write renderer config and install missing renderer dependencies")
	case classifyError(err) == kindServerBusy:
		hints = append(hints, "retry later, raise --workers/--queue, or use async server submission")
	case classifyError(err) == kindSecurity:
		hints = append(hints, "check runtime allow_hosts, allow_paths, offline, and server auth policy")
	case strings.Contains(message, "command is not available") || strings.Contains(message, "executable file not found"):
		hints = append(hints, "run `molstar doctor --fix` to write renderer config and install missing renderer dependencies")
	case strings.Contains(message, "network") || strings.Contains(message, "download") || strings.Contains(message, "offline"):
		hints = append(hints, "check runtime network/offline settings and any allow-host policy")
	case strings.Contains(message, "path") && (strings.Contains(message, "allowed") || strings.Contains(message, "outside")):
		hints = append(hints, "check runtime allow_paths or use an input under an allowed root")
	case strings.Contains(message, "max_pixels") || strings.Contains(message, "pixels"):
		hints = append(hints, "reduce --size or raise --max-pixels for this job")
	case strings.Contains(message, "max_outputs"):
		hints = append(hints, "reduce outputs or raise --max-outputs for this job")
	case strings.Contains(message, "mvsx output requires local or cached input"):
		hints = append(hints, "set runtime.cache or pass --cache so remote inputs can be bundled into the MVSX archive")
	case strings.Contains(message, "portable mvs selectors do not support negation"):
		hints = append(hints, "replace not: selectors with explicit included selectors such as polymer, ligand, ion, or chain:A")
	case classifyError(err) == kindInvalidInput:
		hints = append(hints, "check the command flags and input paths; `molstar --help` lists the flags a command accepts, and `molstar job explain JOB --json` resolves a job's inputs and outputs without rendering")
	case classifyError(err) == kindInvalidScene || classifyError(err) == kindValidation:
		hints = append(hints, "run `molstar job validate` or `molstar inspect JOB --semantic=auto --json` to see what the scene compiles to")
	// Deliberately matches "renderer", not bare "render"/"molstar": those appear in
	// almost every file path this tool touches and produced doctor advice for
	// unrelated failures such as a missing job file.
	case strings.Contains(message, "renderer"):
		hints = append(hints, "run `molstar doctor --json` to verify the renderer dependencies")
	case strings.Contains(message, "schema") || strings.Contains(message, "validation"):
		hints = append(hints, "run `molstar job validate` or `molstar job explain` on the job spec")
	}
	if len(hints) == 0 {
		return nil
	}
	return hints
}

func ExitCode(err error) int {
	switch classifyError(err) {
	case kindInvalidInput:
		return 2
	case kindInvalidScene, kindValidation:
		return 3
	case kindRuntime:
		return 4
	case kindRender, kindRenderer, kindRendererABI:
		return 5
	case kindNetwork:
		return 7
	case kindSecurity:
		return 8
	case kindServerBusy:
		return 9
	case kindDoctor:
		return 6
	case kindCanceled:
		return 130
	default:
		return 1
	}
}

func wantsJSONOnError(args []string) bool {
	for _, arg := range args {
		if arg == "--json" {
			return true
		}
		if strings.HasPrefix(arg, "--json=") {
			value := strings.TrimPrefix(arg, "--json=")
			return value != "false" && value != "0"
		}
	}
	return false
}
