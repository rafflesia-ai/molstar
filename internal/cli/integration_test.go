package cli

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rafflesia-ai/molstar/internal/job"
	"github.com/rafflesia-ai/molstar/internal/render"
)

func TestInstallLocalCommandInstallsBinaryAndConfig(t *testing.T) {
	root := cliRepoRootForTest(t)
	binDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.json")
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	stdout, stderr, err := runAppForTest(ctx,
		"install-local",
		"--home", root,
		"--bin-dir", binDir,
		"--name", "molstar-test",
		"--config", configPath,
		"--force",
		"--json",
	)
	if err != nil {
		t.Fatalf("install-local failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var report installReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("install-local stdout is not JSON: %v\n%s", err, stdout)
	}
	if !report.OK {
		t.Fatalf("install report is not ok: %#v", report)
	}
	if report.Binary != filepath.Join(binDir, "molstar-test") {
		t.Fatalf("binary = %q", report.Binary)
	}
	info, err := os.Stat(report.Binary)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("installed binary is not executable: %s", info.Mode())
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatal(err)
	}

	doctor := exec.CommandContext(ctx, report.Binary, "doctor", "--skip-probe", "--json")
	doctor.Dir = t.TempDir()
	doctor.Env = append(os.Environ(),
		render.ConfigEnv+"="+configPath,
		"MOLSTAR_RENDER=",
		"MOLSTAR_RENDER_FALLBACK=",
		"MOLSTAR_VALIDATE=",
	)
	doctorOutput, err := doctor.CombinedOutput()
	if err != nil {
		t.Fatalf("installed doctor failed: %v\n%s", err, string(doctorOutput))
	}
	var doctorReport doctorReport
	if err := json.Unmarshal(doctorOutput, &doctorReport); err != nil {
		t.Fatalf("doctor stdout is not JSON: %v\n%s", err, string(doctorOutput))
	}
	if !doctorReport.OK {
		t.Fatalf("doctor report is not ok: %#v", doctorReport)
	}

	demoOut := filepath.Join(t.TempDir(), "installed-demo.png")
	demoStdout, demoStderr, err := runInstalledForTest(ctx, report.Binary, configPath, "render", "--demo", "--out", demoOut, "--size", "80x60", "--json")
	if err != nil {
		t.Fatalf("installed demo render failed: %v\nstdout:\n%s\nstderr:\n%s", err, demoStdout, demoStderr)
	}
	if strings.Contains(demoStderr, "Processing ") {
		t.Fatalf("installed JSON demo render should be quiet by default, stderr:\n%s", demoStderr)
	}
	var demoReport renderReport
	if err := json.Unmarshal([]byte(demoStdout), &demoReport); err != nil {
		t.Fatalf("installed demo render stdout is not JSON: %v\n%s", err, demoStdout)
	}
	if !demoReport.OK || len(demoReport.CachedInputs) != 0 || len(demoReport.OutputFiles) != 1 || demoReport.OutputFiles[0].Width != 80 || demoReport.OutputFiles[0].Height != 60 {
		t.Fatalf("unexpected installed demo report: %#v", demoReport)
	}

	fallbackOut := filepath.Join(t.TempDir(), "installed-fallback.png")
	fallbackStdout, fallbackStderr, err := runInstalledForTest(ctx, report.Binary, configPath, "render", "--demo", "--out", fallbackOut, "--size", "80x60", "--renderer-command", "false", "--json")
	if err != nil {
		t.Fatalf("installed fallback render failed: %v\nstdout:\n%s\nstderr:\n%s", err, fallbackStdout, fallbackStderr)
	}
	var fallbackReport renderReport
	if err := json.Unmarshal([]byte(fallbackStdout), &fallbackReport); err != nil {
		t.Fatalf("installed fallback render stdout is not JSON: %v\n%s", err, fallbackStdout)
	}
	if len(fallbackReport.Commands) != 1 || len(fallbackReport.Commands[0].FallbackOf) == 0 {
		t.Fatalf("expected fallback command metadata, got %#v\nstderr:\n%s", fallbackReport.Commands, fallbackStderr)
	}

	selfStdout, selfStderr, err := runInstalledForTest(ctx, report.Binary, configPath, "self-test", "--json", "--timeout", "180s")
	if err != nil {
		t.Fatalf("installed self-test failed: %v\nstdout:\n%s\nstderr:\n%s", err, selfStdout, selfStderr)
	}
	var selfReport selfTestReport
	if err := json.Unmarshal([]byte(selfStdout), &selfReport); err != nil {
		t.Fatalf("self-test stdout is not JSON: %v\n%s", err, selfStdout)
	}
	if !selfReport.OK || len(selfReport.Steps) == 0 {
		t.Fatalf("unexpected self-test report: %#v\nstderr:\n%s", selfReport, selfStderr)
	}
}

func TestInstallArtifactCommandInstallsUnpackedRuntime(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "artifact")
	writeFakeArtifactRuntime(t, artifact)
	runtimeRoot := filepath.Join(dir, "runtime")
	binDir := filepath.Join(dir, "bin")
	configPath := filepath.Join(dir, "config.json")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stdout, stderr, err := runAppForTest(ctx,
		"install-artifact",
		"--artifact", artifact,
		"--prefix", runtimeRoot,
		"--bin-dir", binDir,
		"--config", configPath,
		"--name", "molstar-artifact",
		"--install-deps=false",
		// This artifact is a stub that exercises unpack/copy/config mechanics; it
		// has no working renderer, so skip the post-install capability probe.
		"--verify=false",
		"--force",
		"--json",
	)
	if err != nil {
		t.Fatalf("install-artifact failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var report installReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("install-artifact stdout is not JSON: %v\n%s", err, stdout)
	}
	if !report.OK || report.Binary != filepath.Join(binDir, "molstar-artifact") || report.Home != runtimeRoot {
		t.Fatalf("unexpected install-artifact report: %#v", report)
	}
	if _, err := os.Stat(report.Binary); err != nil {
		t.Fatal(err)
	}
	var config render.RuntimeConfig
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config.Home != runtimeRoot || !strings.Contains(strings.Join(config.RendererCommand, " "), runtimeRoot) {
		t.Fatalf("unexpected runtime config: %#v", config)
	}
}

func TestInstallArtifactCommandInstallsZipRuntime(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "artifact")
	writeFakeArtifactRuntime(t, artifact)
	archivePath := filepath.Join(dir, "artifact.zip")
	writeZipForTest(t, archivePath, artifact, "headlessmolstar-test")

	runtimeRoot := filepath.Join(dir, "runtime")
	binDir := filepath.Join(dir, "bin")
	configPath := filepath.Join(dir, "config.json")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stdout, stderr, err := runAppForTest(ctx,
		"install-artifact",
		"--artifact", archivePath,
		"--prefix", runtimeRoot,
		"--bin-dir", binDir,
		"--config", configPath,
		"--name", "molstar-zip",
		"--install-deps=false",
		// This artifact is a stub that exercises unpack/copy/config mechanics; it
		// has no working renderer, so skip the post-install capability probe.
		"--verify=false",
		"--force",
		"--json",
	)
	if err != nil {
		t.Fatalf("install-artifact zip failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var report installReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("install-artifact zip stdout is not JSON: %v\n%s", err, stdout)
	}
	if !report.OK || report.Binary != filepath.Join(binDir, "molstar-zip") {
		t.Fatalf("unexpected install-artifact zip report: %#v", report)
	}
	if _, err := os.Stat(filepath.Join(report.Home, "scripts", "render-mvs.js")); err != nil {
		t.Fatal(err)
	}
	var config render.RuntimeConfig
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config.Home != report.Home || !strings.Contains(strings.Join(config.RendererCommand, " "), report.Home) {
		t.Fatalf("unexpected zip runtime config: %#v", config)
	}
}

func TestRenderJSONSuppressesRendererProgressUnlessVerbose(t *testing.T) {
	root := cliRepoRootForTest(t)
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := render.WriteRuntimeConfig(configPath, render.RuntimeConfigForHome(root)); err != nil {
		t.Fatal(err)
	}
	t.Setenv(render.ConfigEnv, configPath)
	t.Setenv("MOLSTAR_RENDER", "")
	t.Setenv("MOLSTAR_RENDER_FALLBACK", "")
	t.Setenv("MOLSTAR_VALIDATE", "")
	chdirForCLITest(t, t.TempDir())

	dir := t.TempDir()
	cifPath := filepath.Join(dir, "one.cif")
	if err := os.WriteFile(cifPath, []byte(oneAtomCIF), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "chain-theme.png")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	stdout, stderr, err := runAppForTest(ctx,
		"render", cifPath,
		"--out", output,
		"--size", "96x72",
		"--select", "all",
		"--repr", "spacefill",
		"--color", "chain",
		"--focus", "all",
		"--json",
	)
	if err != nil {
		t.Fatalf("render failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !json.Valid([]byte(stdout)) {
		t.Fatalf("render stdout is not valid JSON:\n%s", stdout)
	}
	if strings.HasPrefix(strings.TrimSpace(stdout), "Processing ") {
		t.Fatalf("raw renderer progress preceded JSON stdout:\n%s", stdout)
	}
	if strings.Contains(stderr, "Processing ") {
		t.Fatalf("renderer progress should be suppressed by default with --json, got:\n%s", stderr)
	}
	var report renderReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("render stdout is not JSON: %v\n%s", err, stdout)
	}
	if !report.OK {
		t.Fatalf("render report is not ok: %#v", report)
	}
	if len(report.OutputFiles) != 1 {
		t.Fatalf("expected one output report, got %#v", report.OutputFiles)
	}
	if got := report.OutputFiles[0]; !got.Verified || !got.NonBlank || got.Width != 96 || got.Height != 72 {
		t.Fatalf("unexpected output verification report: %#v", got)
	}

	verboseOutput := filepath.Join(dir, "chain-theme-verbose.png")
	stdout, stderr, err = runAppForTest(ctx,
		"render", cifPath,
		"--out", verboseOutput,
		"--size", "96x72",
		"--select", "all",
		"--repr", "spacefill",
		"--color", "chain",
		"--focus", "all",
		"--json",
		"--verbose",
	)
	if err != nil {
		t.Fatalf("verbose render failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stderr, "Processing ") {
		t.Fatalf("expected renderer progress with --verbose, got:\n%s", stderr)
	}
}

func TestRenderDiagnoseForcesJSONReport(t *testing.T) {
	output := filepath.Join(t.TempDir(), "diagnose.png")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	stdout, stderr, err := runAppForTest(ctx,
		"render",
		"--demo",
		"--dry-run",
		"--diagnose",
		"--out", output,
		"--size", "64x64",
	)
	if err != nil {
		t.Fatalf("render diagnose failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var report renderReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("diagnose stdout is not JSON: %v\n%s", err, stdout)
	}
	if !report.OK || len(report.Commands) != 1 || !report.Commands[0].Skipped {
		t.Fatalf("unexpected diagnose report: %#v", report)
	}
	capabilities, ok := report.Diagnostics["capabilities"].(map[string]any)
	if !ok || capabilities["ok"] != true {
		t.Fatalf("missing renderer capabilities: %#v", report.Diagnostics)
	}
}

func TestDoctorJSONReportsConfigAndRendererProbeStatus(t *testing.T) {
	root := cliRepoRootForTest(t)
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := render.WriteRuntimeConfig(configPath, render.RuntimeConfigForHome(root)); err != nil {
		t.Fatal(err)
	}
	t.Setenv(render.ConfigEnv, configPath)
	t.Setenv("MOLSTAR_RENDER", "")
	t.Setenv("MOLSTAR_RENDER_FALLBACK", "")
	t.Setenv("MOLSTAR_VALIDATE", "")
	chdirForCLITest(t, t.TempDir())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	stdout, stderr, err := runAppForTest(ctx, "doctor", "--json")
	if err != nil {
		t.Fatalf("doctor failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var report doctorReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("doctor stdout is not JSON: %v\n%s", err, stdout)
	}
	if !report.OK || !report.Config.Loaded || report.Config.Path != configPath {
		t.Fatalf("unexpected doctor config report: %#v", report)
	}
	if !report.Renderer.Primary.Available || !report.Renderer.Primary.Tested || !report.Renderer.Primary.OK {
		t.Fatalf("primary renderer was not fully reported/tested: %#v", report.Renderer.Primary)
	}
	if !report.Renderer.Fallback.Available || !report.Renderer.Fallback.Tested || !report.Renderer.Fallback.OK {
		t.Fatalf("fallback renderer was not fully reported/tested: %#v", report.Renderer.Fallback)
	}
	if !report.Renderer.Validate.Available {
		t.Fatalf("validator was not reported available: %#v", report.Renderer.Validate)
	}
}

func TestDoctorFixWritesConfigAndCache(t *testing.T) {
	root := cliRepoRootForTest(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	cacheDir := filepath.Join(dir, "cache")
	t.Setenv(render.ConfigEnv, configPath)
	t.Setenv("MOLSTAR_RENDER", "")
	t.Setenv("MOLSTAR_RENDER_FALLBACK", "")
	t.Setenv("MOLSTAR_VALIDATE", "")
	chdirForCLITest(t, t.TempDir())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stdout, stderr, err := runAppForTest(ctx,
		"doctor",
		"--fix",
		"--home", root,
		"--config", configPath,
		"--cache", cacheDir,
		"--skip-probe",
		"--json",
	)
	if err != nil {
		t.Fatalf("doctor --fix failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var report doctorReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("doctor --fix stdout is not JSON: %v\n%s", err, stdout)
	}
	if !report.OK || !report.Config.Loaded || report.Config.Path != configPath {
		t.Fatalf("unexpected doctor --fix report: %#v", report)
	}
	if len(report.Fixes) == 0 {
		t.Fatalf("expected doctor fixes in report: %#v", report)
	}
	if err := requireNonEmptyFile(configPath); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(cacheDir); err != nil || !info.IsDir() {
		t.Fatalf("expected cache dir %s: %v", cacheDir, err)
	}
}

func TestCapabilitiesJSONReportsRendererTypes(t *testing.T) {
	root := cliRepoRootForTest(t)
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := render.WriteRuntimeConfig(configPath, render.RuntimeConfigForHome(root)); err != nil {
		t.Fatal(err)
	}
	t.Setenv(render.ConfigEnv, configPath)
	t.Setenv("MOLSTAR_RENDER", "")
	t.Setenv("MOLSTAR_RENDER_FALLBACK", "")
	t.Setenv("MOLSTAR_VALIDATE", "")
	chdirForCLITest(t, t.TempDir())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stdout, stderr, err := runAppForTest(ctx, "capabilities", "--json")
	if err != nil {
		t.Fatalf("capabilities failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var report capabilitiesReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("capabilities stdout is not JSON: %v\n%s", err, stdout)
	}
	if !report.OK || report.Runtime.Node == "" || report.Runtime.Molstar == "" {
		t.Fatalf("unexpected runtime capabilities: %#v", report)
	}
	if report.Renderer.Primary.Kind != "owned-renderer" || !report.Renderer.Primary.SupportsCapabilities || !report.Worker.Supported {
		t.Fatalf("unexpected renderer capabilities: %#v", report.Renderer.Primary)
	}
	if report.Renderer.Primary.Capabilities == nil || report.Renderer.Primary.Capabilities.Renderer["gl"] == nil || report.Renderer.Primary.Capabilities.Renderer["canvas"] == nil {
		t.Fatalf("missing GL/canvas capability probes: %#v", report.Renderer.Primary.Capabilities)
	}
	if len(report.Matrix) < 4 || report.Matrix[0].Target != "primary" || report.Matrix[0].WebGL == "" {
		t.Fatalf("unexpected capability matrix: %#v", report.Matrix)
	}
}

func TestVersionJSONReportsCLIAndRuntime(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stdout, stderr, err := runAppForTest(ctx, "version", "--json", "--skip-runtime")
	if err != nil {
		t.Fatalf("version failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var report versionReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("version stdout is not JSON: %v\n%s", err, stdout)
	}
	if !report.OK || report.CLI.Version == "" || report.CLI.GoVersion == "" || report.CLI.GOOS == "" || report.CLI.GOARCH == "" {
		t.Fatalf("unexpected version report: %#v", report)
	}
	if report.Runtime != nil {
		t.Fatalf("runtime probe should be omitted with --skip-runtime: %#v", report.Runtime)
	}
}

func TestUpdateRuntimeSkipInstallWritesConfigAndProbes(t *testing.T) {
	root := cliRepoRootForTest(t)
	configPath := filepath.Join(t.TempDir(), "config.json")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	stdout, stderr, err := runAppForTest(ctx, "update-runtime", "--home", root, "--config", configPath, "--skip-install", "--json")
	if err != nil {
		t.Fatalf("update-runtime failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var report updateRuntimeReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("update-runtime stdout is not JSON: %v\n%s", err, stdout)
	}
	if !report.OK || report.Home != root || report.Config != configPath || report.Capabilities == nil || !report.Capabilities.OK {
		t.Fatalf("unexpected update-runtime report: %#v", report)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatal(err)
	}
}

func TestCompletionAndDocsCommandsWriteFiles(t *testing.T) {
	dir := t.TempDir()
	completions := filepath.Join(dir, "completions")
	docs := filepath.Join(dir, "docs")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stdout, stderr, err := runAppForTest(ctx, "completion", "all", "--out-dir", completions)
	if err != nil {
		t.Fatalf("completion failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	for _, name := range []string{"molstar.bash", "_molstar", "molstar.fish", "molstar.ps1"} {
		if err := requireNonEmptyFile(filepath.Join(completions, name)); err != nil {
			t.Fatal(err)
		}
	}
	stdout, stderr, err = runAppForTest(ctx, "docs", "--out", docs)
	if err != nil {
		t.Fatalf("docs failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if err := requireNonEmptyFile(filepath.Join(docs, "molstar.md")); err != nil {
		t.Fatal(err)
	}
	if err := requireNonEmptyFile(filepath.Join(docs, "molstar_render.md")); err != nil {
		t.Fatal(err)
	}
}

func TestRenderDemoIsLocalAndQuiet(t *testing.T) {
	output := filepath.Join(t.TempDir(), "demo.png")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	stdout, stderr, err := runAppForTest(ctx, "render", "--demo", "--out", output, "--size", "96x72", "--json")
	if err != nil {
		t.Fatalf("demo render failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if strings.Contains(stderr, "Processing ") {
		t.Fatalf("demo render should be quiet with --json, got stderr:\n%s", stderr)
	}
	var report renderReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("demo render stdout is not JSON: %v\n%s", err, stdout)
	}
	if !report.OK || len(report.CachedInputs) != 0 || len(report.OutputFiles) != 1 {
		t.Fatalf("unexpected demo render report: %#v", report)
	}
	if got := report.OutputFiles[0]; !got.Verified || !got.NonBlank || got.Width != 96 || got.Height != 72 {
		t.Fatalf("unexpected demo output report: %#v", got)
	}
	if err := checkOutputReportAgainstGolden(report.OutputFiles[0], demoVisualGolden); err != nil {
		t.Fatalf("demo golden mismatch: %v", err)
	}

	sceneDir := t.TempDir()
	scenePath := filepath.Join(sceneDir, "demo.mvsj")
	sceneOutput := filepath.Join(sceneDir, "demo-source.png")
	stdout, stderr, err = runAppForTest(ctx, "render", "--demo", "--out", sceneOutput, "--size", "96x72", "--mvs", scenePath, "--json")
	if err != nil {
		t.Fatalf("demo render with MVS output failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if err := requireNonEmptyFile(filepath.Join(sceneDir, "demo-assets", "demo.cif")); err != nil {
		t.Fatal(err)
	}
	rerenderedSceneOutput := filepath.Join(sceneDir, "from-demo-mvs.png")
	stdout, stderr, err = runAppForTest(ctx, "render", scenePath, "--out", rerenderedSceneOutput, "--size", "96x72", "--json")
	if err != nil {
		t.Fatalf("rendering saved demo MVS failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("saved MVS render stdout is not JSON: %v\n%s", err, stdout)
	}
	if !report.OK || len(report.OutputFiles) == 0 || !report.OutputFiles[0].Verified {
		t.Fatalf("unexpected saved MVS render report: %#v", report)
	}

	summaryOutput := filepath.Join(t.TempDir(), "demo-summary.png")
	stdout, stderr, err = runAppForTest(ctx, "render", "--demo", "--out", summaryOutput, "--size", "96x72", "--show-report")
	if err != nil {
		t.Fatalf("demo render --show-report failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "render\tok") || !strings.Contains(stdout, "output\t"+summaryOutput) || strings.HasPrefix(strings.TrimSpace(stdout), "{") {
		t.Fatalf("unexpected render summary:\n%s", stdout)
	}

	explainStdout, explainStderr, err := runAppForTest(ctx, "render", "--demo", "--explain", "--json")
	if err != nil {
		t.Fatalf("render --explain failed: %v\nstdout:\n%s\nstderr:\n%s", err, explainStdout, explainStderr)
	}
	var explain renderExplainReport
	if err := json.Unmarshal([]byte(explainStdout), &explain); err != nil {
		t.Fatalf("render --explain stdout is not JSON: %v\n%s", err, explainStdout)
	}
	if !explain.OK || explain.Kind != "demo" || explain.MVSNodeCount == 0 || len(explain.Outputs) != 1 {
		t.Fatalf("unexpected render explain report: %#v", explain)
	}

	runDir := t.TempDir()
	t.Setenv("MOLSTAR_RUNS_DIR", runDir)
	loggedOutput := filepath.Join(t.TempDir(), "logged-demo.png")
	stdout, stderr, err = runAppForTest(ctx, "render", "--demo", "--out", loggedOutput, "--size", "96x72", "--run-label", "ci-demo", "--json")
	if err != nil {
		t.Fatalf("logged demo render failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var loggedRender renderReport
	if err := json.Unmarshal([]byte(stdout), &loggedRender); err != nil {
		t.Fatalf("logged render stdout is not JSON: %v\n%s", err, stdout)
	}
	if loggedRender.RunLog == "" || loggedRender.RunLabel != "ci-demo" {
		t.Fatalf("render did not report run log and label: %#v", loggedRender)
	}
	stdout, stderr, err = runAppForTest(ctx, "logs", "--last", "--dir", runDir, "--json")
	if err != nil {
		t.Fatalf("logs --last failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var logReport struct {
		OK  bool           `json:"ok"`
		Run runLogEnvelope `json:"run"`
	}
	if err := json.Unmarshal([]byte(stdout), &logReport); err != nil {
		t.Fatalf("logs stdout is not JSON: %v\n%s", err, stdout)
	}
	if !logReport.OK || !logReport.Run.Report.OK || logReport.Run.Label != "ci-demo" || len(logReport.Run.Report.OutputFiles) != 1 {
		t.Fatalf("unexpected run log report: %#v", logReport)
	}
	if !logReport.Run.Replay.Replayable || !logReport.Run.Replay.FullyReplayable || len(logReport.Run.Assets) == 0 {
		t.Fatalf("expected fully replayable run log with embedded inputs: %#v", logReport.Run.Replay)
	}
	runID := logReport.Run.ID
	stdout, stderr, err = runAppForTest(ctx, "logs", "list", "--dir", runDir, "--limit", "5", "--json")
	if err != nil {
		t.Fatalf("logs list failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var listReport struct {
		OK   bool            `json:"ok"`
		Runs []runLogSummary `json:"runs"`
	}
	if err := json.Unmarshal([]byte(stdout), &listReport); err != nil {
		t.Fatalf("logs list stdout is not JSON: %v\n%s", err, stdout)
	}
	if !listReport.OK || len(listReport.Runs) != 1 || listReport.Runs[0].Label != "ci-demo" || listReport.Runs[0].FirstOutput != loggedOutput || !listReport.Runs[0].Replayable || !listReport.Runs[0].FullyReplayable {
		t.Fatalf("unexpected logs list report: %#v", listReport)
	}
	stdout, stderr, err = runAppForTest(ctx, "logs", "show", runID, "--dir", runDir, "--json")
	if err != nil {
		t.Fatalf("logs show failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &logReport); err != nil {
		t.Fatalf("logs show stdout is not JSON: %v\n%s", err, stdout)
	}
	if logReport.Run.ID != runID || logReport.Run.Label != "ci-demo" {
		t.Fatalf("unexpected logs show report: %#v", logReport)
	}
	rerunDir := filepath.Join(t.TempDir(), "rerun")
	stdout, stderr, err = runAppForTest(ctx, "logs", "show", runID, "--dir", runDir, "--rerun", "--out-dir", rerunDir, "--json")
	if err != nil {
		t.Fatalf("logs show --rerun failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var rerunReport struct {
		OK    bool         `json:"ok"`
		Rerun renderReport `json:"rerun"`
	}
	if err := json.Unmarshal([]byte(stdout), &rerunReport); err != nil {
		t.Fatalf("rerun stdout is not JSON: %v\n%s", err, stdout)
	}
	if !rerunReport.OK || !rerunReport.Rerun.OK || len(rerunReport.Rerun.OutputFiles) == 0 {
		t.Fatalf("unexpected rerun report: %#v", rerunReport)
	}
	if err := requireNonEmptyFile(filepath.Join(rerunDir, filepath.Base(loggedOutput))); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = runAppForTest(ctx, "diagnose", runID, "--dir", runDir, "--json")
	if err != nil {
		t.Fatalf("diagnose run failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var diagnosis diagnoseReport
	if err := json.Unmarshal([]byte(stdout), &diagnosis); err != nil {
		t.Fatalf("diagnose stdout is not JSON: %v\n%s", err, stdout)
	}
	if !diagnosis.OK || diagnosis.Failed || !diagnosis.Replayable || !diagnosis.FullyReplayable || !strings.Contains(diagnosis.NextCommand, "logs show") {
		t.Fatalf("unexpected successful run diagnosis: %#v", diagnosis)
	}
	bundlePath := filepath.Join(t.TempDir(), "run.molrun")
	stdout, stderr, err = runAppForTest(ctx, "logs", "export", runID, "--dir", runDir, "--out", bundlePath, "--json")
	if err != nil {
		t.Fatalf("logs export failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if err := requireNonEmptyFile(bundlePath); err != nil {
		t.Fatal(err)
	}
	var exportReport struct {
		OK     bool         `json:"ok"`
		Replay runLogReplay `json:"replay"`
	}
	if err := json.Unmarshal([]byte(stdout), &exportReport); err != nil {
		t.Fatalf("logs export stdout is not JSON: %v\n%s", err, stdout)
	}
	if !exportReport.OK || !exportReport.Replay.Replayable || !exportReport.Replay.FullyReplayable {
		t.Fatalf("unexpected portable export report: %#v", exportReport)
	}
	stdout, stderr, err = runAppForTest(ctx, "logs", "verify", bundlePath, "--json")
	if err != nil {
		t.Fatalf("logs verify failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var verifyReport runLogVerifyReport
	if err := json.Unmarshal([]byte(stdout), &verifyReport); err != nil {
		t.Fatalf("logs verify stdout is not JSON: %v\n%s", err, stdout)
	}
	if !verifyReport.OK || !verifyReport.Replayable || !verifyReport.FullyReplayable || verifyReport.ID != runID || len(verifyReport.Files) == 0 || len(verifyReport.ExpectedOutputs) == 0 {
		t.Fatalf("unexpected logs verify report: %#v", verifyReport)
	}
	bundleRerunDir := filepath.Join(t.TempDir(), "bundle-rerun")
	stdout, stderr, err = runAppForTest(ctx, "logs", "rerun", bundlePath, "--out-dir", bundleRerunDir, "--json")
	if err != nil {
		t.Fatalf("logs rerun failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &rerunReport); err != nil {
		t.Fatalf("logs rerun stdout is not JSON: %v\n%s", err, stdout)
	}
	if !rerunReport.OK || !rerunReport.Rerun.OK || len(rerunReport.Rerun.OutputFiles) == 0 {
		t.Fatalf("unexpected logs rerun report: %#v", rerunReport)
	}
	if err := requireNonEmptyFile(filepath.Join(bundleRerunDir, filepath.Base(loggedOutput))); err != nil {
		t.Fatal(err)
	}
	successIssueBundle := filepath.Join(t.TempDir(), "success-issue.zip")
	stdout, stderr, err = runAppForTest(ctx, "diagnose", runID, "--dir", runDir, "--bundle", "--out", successIssueBundle, "--json")
	if err != nil {
		t.Fatalf("diagnose --bundle success failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	requireZipEntries(t, successIssueBundle, "diagnosis.json", "redactions.json", "run.json", "render-report.json", "job.json", "scene.mvsj", "doctor.json", "capabilities.json")
	redactedIssueBundle := filepath.Join(t.TempDir(), "redacted-issue.zip")
	stdout, stderr, err = runAppForTest(ctx, "diagnose", runID, "--dir", runDir, "--bundle", "--out", redactedIssueBundle, "--redact-paths", "--redact-env", "--redact-inputs", "--json")
	if err != nil {
		t.Fatalf("diagnose --bundle redacted failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	requireZipEntries(t, redactedIssueBundle, "diagnosis.json", "redactions.json", "run.json", "render-report.json", "job.json", "scene.mvsj", "doctor.json", "capabilities.json")
	requireZipEntryContains(t, redactedIssueBundle, "redactions.json", `"paths": true`)
	requireZipEntryContains(t, redactedIssueBundle, "redactions.json", `"inputs": true`)
	requireZipEntryNotContains(t, redactedIssueBundle, "run.json", filepath.Dir(loggedOutput))
	importDir := t.TempDir()
	stdout, stderr, err = runAppForTest(ctx, "logs", "import", bundlePath, "--dir", importDir, "--json")
	if err != nil {
		t.Fatalf("logs import failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	stdout, stderr, err = runAppForTest(ctx, "logs", "show", runID, "--dir", importDir, "--json")
	if err != nil {
		t.Fatalf("logs show imported run failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	importRerunDir := filepath.Join(t.TempDir(), "import-rerun")
	stdout, stderr, err = runAppForTest(ctx, "logs", "show", runID, "--dir", importDir, "--rerun", "--out-dir", importRerunDir, "--json")
	if err != nil {
		t.Fatalf("logs show imported run --rerun failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if err := requireNonEmptyFile(filepath.Join(importRerunDir, filepath.Base(loggedOutput))); err != nil {
		t.Fatal(err)
	}
	noInputsBundlePath := filepath.Join(t.TempDir(), "run-no-inputs.molrun")
	stdout, stderr, err = runAppForTest(ctx, "logs", "export", runID, "--dir", runDir, "--out", noInputsBundlePath, "--include-inputs=false", "--json")
	if err != nil {
		t.Fatalf("logs export --include-inputs=false failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &exportReport); err != nil {
		t.Fatalf("logs export no-inputs stdout is not JSON: %v\n%s", err, stdout)
	}
	if exportReport.Replay.Replayable || len(exportReport.Replay.MissingInputs) == 0 || len(exportReport.Replay.Warnings) == 0 {
		t.Fatalf("expected non-replayable no-inputs export report: %#v", exportReport)
	}
	stdout, stderr, err = runAppForTest(ctx, "logs", "verify", noInputsBundlePath, "--json")
	if err != nil {
		t.Fatalf("logs verify no-inputs failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &verifyReport); err != nil {
		t.Fatalf("logs verify no-inputs stdout is not JSON: %v\n%s", err, stdout)
	}
	if verifyReport.Replayable || verifyReport.FullyReplayable || len(verifyReport.Warnings) == 0 {
		t.Fatalf("expected non-replayable no-inputs verify report: %#v", verifyReport)
	}
	stdout, stderr, err = runAppForTest(ctx, "logs", "verify", noInputsBundlePath, "--strict", "--json")
	if err == nil {
		t.Fatalf("expected logs verify --strict to fail for no-inputs bundle\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if strings.TrimSpace(stderr) != "" {
		t.Fatalf("logs verify --strict JSON should keep stderr empty, got %q", stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &verifyReport); err != nil {
		t.Fatalf("logs verify strict no-inputs stdout is not JSON: %v\n%s", err, stdout)
	}
	if verifyReport.OK || verifyReport.Replayable {
		t.Fatalf("expected strict no-inputs verify report to be not ok and not replayable: %#v", verifyReport)
	}
	noInputsDir := t.TempDir()
	stdout, stderr, err = runAppForTest(ctx, "logs", "import", noInputsBundlePath, "--dir", noInputsDir, "--json")
	if err != nil {
		t.Fatalf("logs import no-inputs bundle failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	stdout, stderr, err = runAppForTest(ctx, "logs", "show", runID, "--dir", noInputsDir, "--rerun", "--out-dir", filepath.Join(t.TempDir(), "no-inputs-rerun"), "--json")
	if err == nil {
		t.Fatalf("expected no-inputs imported rerun to fail\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	stdout, stderr, err = runAppForTest(ctx, "diagnose", runID, "--dir", noInputsDir, "--json")
	if err != nil {
		t.Fatalf("diagnose no-inputs run failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &diagnosis); err != nil {
		t.Fatalf("diagnose no-inputs stdout is not JSON: %v\n%s", err, stdout)
	}
	if diagnosis.Replayable || diagnosis.FullyReplayable || !strings.Contains(diagnosis.NextCommand, "logs export") {
		t.Fatalf("unexpected no-inputs diagnosis: %#v", diagnosis)
	}
	noLogOutput := filepath.Join(t.TempDir(), "no-log-demo.png")
	stdout, stderr, err = runAppForTest(ctx, "render", "--demo", "--out", noLogOutput, "--size", "96x72", "--no-log", "--json")
	if err != nil {
		t.Fatalf("render --no-log failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var noLogRender renderReport
	if err := json.Unmarshal([]byte(stdout), &noLogRender); err != nil {
		t.Fatalf("no-log render stdout is not JSON: %v\n%s", err, stdout)
	}
	if noLogRender.RunLog != "" {
		t.Fatalf("render --no-log reported a run log: %#v", noLogRender)
	}
	ciDir := filepath.Join(t.TempDir(), "ci")
	stdout, stderr, err = runAppForTest(ctx, "render", "--demo", "--focus", "missing-component", "--ci-artifact", ciDir, "--json")
	if err == nil {
		t.Fatalf("expected render with invalid focus to fail\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	for _, path := range []string{"ci-artifact.json", "render-report.json", "job.json", "explain.json", "doctor.json"} {
		if err := requireNonEmptyFile(filepath.Join(ciDir, path)); err != nil {
			t.Fatal(err)
		}
	}
	stdout, stderr, err = runAppForTest(ctx, "logs", "list", "--dir", runDir, "--failed", "--json")
	if err != nil {
		t.Fatalf("logs list --failed failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &listReport); err != nil {
		t.Fatalf("logs list --failed stdout is not JSON: %v\n%s", err, stdout)
	}
	if len(listReport.Runs) != 1 || listReport.Runs[0].OK {
		t.Fatalf("expected one failed run, got %#v", listReport)
	}
	failedRunID := listReport.Runs[0].ID
	stdout, stderr, err = runAppForTest(ctx, "diagnose", failedRunID, "--dir", runDir, "--json")
	if err != nil {
		t.Fatalf("diagnose failed run failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &diagnosis); err != nil {
		t.Fatalf("diagnose failed run stdout is not JSON: %v\n%s", err, stdout)
	}
	if !diagnosis.OK || !diagnosis.Failed || diagnosis.Error == nil || diagnosis.LikelyCause == "" || diagnosis.RendererStatus == "" {
		t.Fatalf("unexpected failed run diagnosis: %#v", diagnosis)
	}
	issueBundle := filepath.Join(t.TempDir(), "issue.zip")
	stdout, stderr, err = runAppForTest(ctx, "diagnose", failedRunID, "--dir", runDir, "--ci-artifact", ciDir, "--bundle", "--out", issueBundle, "--json")
	if err != nil {
		t.Fatalf("diagnose --bundle failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var issueReport struct {
		OK     bool     `json:"ok"`
		Output string   `json:"output"`
		Files  []string `json:"files"`
	}
	if err := json.Unmarshal([]byte(stdout), &issueReport); err != nil {
		t.Fatalf("diagnose --bundle stdout is not JSON: %v\n%s", err, stdout)
	}
	if !issueReport.OK || issueReport.Output != issueBundle || len(issueReport.Files) == 0 {
		t.Fatalf("unexpected diagnose bundle report: %#v", issueReport)
	}
	requireZipEntries(t, issueBundle, "diagnosis.json", "redactions.json", "run.json", "run-log.json", "render-report.json", "job.json", "doctor.json", "capabilities.json", "ci-artifact/ci-artifact.json", "ci-artifact/render-report.json", "ci-artifact/job.json")
	stdout, stderr, err = runAppForTest(ctx, "diagnose", "--ci-artifact", ciDir, "--json")
	if err != nil {
		t.Fatalf("diagnose CI artifact failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &diagnosis); err != nil {
		t.Fatalf("diagnose CI artifact stdout is not JSON: %v\n%s", err, stdout)
	}
	if !diagnosis.OK || diagnosis.Source != "ci_artifact" || !diagnosis.Failed || diagnosis.Error == nil || diagnosis.NextCommand == "" || len(diagnosis.ArtifactFiles) == 0 {
		t.Fatalf("unexpected CI artifact diagnosis: %#v", diagnosis)
	}
	stdout, stderr, err = runAppForTest(ctx, "logs", "prune", "--dir", runDir, "--older-than", "0s", "--dry-run", "--json")
	if err != nil {
		t.Fatalf("logs prune dry-run failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var pruneReport struct {
		OK      bool     `json:"ok"`
		DryRun  bool     `json:"dry_run"`
		Removed []string `json:"removed"`
	}
	if err := json.Unmarshal([]byte(stdout), &pruneReport); err != nil {
		t.Fatalf("logs prune stdout is not JSON: %v\n%s", err, stdout)
	}
	if !pruneReport.OK || !pruneReport.DryRun || len(pruneReport.Removed) != 2 {
		t.Fatalf("unexpected prune dry-run report: %#v", pruneReport)
	}
	stdout, stderr, err = runAppForTest(ctx, "logs", "prune", "--dir", runDir, "--older-than", "0s", "--json")
	if err != nil {
		t.Fatalf("logs prune failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &pruneReport); err != nil {
		t.Fatalf("logs prune stdout is not JSON: %v\n%s", err, stdout)
	}
	if !pruneReport.OK || len(pruneReport.Removed) != 2 {
		t.Fatalf("unexpected prune report: %#v", pruneReport)
	}
	stdout, stderr, err = runAppForTest(ctx, "logs", "list", "--dir", runDir, "--json")
	if err != nil {
		t.Fatalf("logs list after prune failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &listReport); err != nil {
		t.Fatalf("logs list after prune stdout is not JSON: %v\n%s", err, stdout)
	}
	if len(listReport.Runs) != 0 {
		t.Fatalf("expected pruned run logs, got %#v", listReport)
	}
}

func TestQuickstartCommand(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	stdout, stderr, err := runAppForTest(ctx, "quickstart", "--out", dir, "--json")
	if err != nil {
		t.Fatalf("quickstart failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var report quickstartReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("quickstart stdout is not JSON: %v\n%s", err, stdout)
	}
	if !report.OK || report.Render == nil || !report.Render.OK || len(report.Next) == 0 {
		t.Fatalf("unexpected quickstart report: %#v", report)
	}
	if err := requireNonEmptyFile(filepath.Join(dir, "demo.png")); err != nil {
		t.Fatal(err)
	}
}

func TestRenderWorkerModeUsesWorkerProtocol(t *testing.T) {
	output := filepath.Join(t.TempDir(), "worker.png")
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	stdout, stderr, err := runAppForTest(ctx, "render", "--demo", "--out", output, "--size", "96x72", "--renderer-mode", "worker", "--json")
	if err != nil {
		t.Fatalf("worker render failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var report renderReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("worker render stdout is not JSON: %v\n%s", err, stdout)
	}
	if !report.OK || len(report.Commands) != 1 || !report.Commands[0].Worker {
		t.Fatalf("expected worker command result, got %#v", report.Commands)
	}
	if report.Diagnostics["renderer_mode"] != "worker" {
		t.Fatalf("expected worker diagnostics, got %#v", report.Diagnostics)
	}
}

func TestBatchWorkerModeReusesWorkerPool(t *testing.T) {
	dir := t.TempDir()
	cifPath := filepath.Join(dir, "one.cif")
	if err := os.WriteFile(cifPath, []byte(oneAtomCIF), 0o644); err != nil {
		t.Fatal(err)
	}
	jobPath := filepath.Join(dir, "jobs.jsonl")
	var lines []string
	for i := 0; i < 2; i++ {
		spec := map[string]any{
			"version": 1,
			"inputs": map[string]any{
				"input": map[string]any{"path": cifPath, "format": "mmcif"},
			},
			"scene": map[string]any{
				"canvas": map[string]any{"background": "white"},
				"structures": []any{map[string]any{
					"source": "input",
					"components": []any{map[string]any{
						"ref": "all", "select": "all",
						"representation": map[string]any{"type": "spacefill", "color": "#cc3399"},
					}},
				}},
				"camera": map[string]any{"focus": "all"},
			},
			"outputs": []any{map[string]any{"type": "image", "path": filepath.Join(dir, fmt.Sprintf("batch-worker-%d.png", i)), "size": []int{96, 72}}},
		}
		data, err := json.Marshal(spec)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(data))
	}
	if err := os.WriteFile(jobPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	stdout, stderr, err := runAppForTest(ctx, "batch", jobPath, "--concurrency", "2", "--renderer-mode", "worker", "--json")
	if err != nil {
		t.Fatalf("batch worker failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	parsed := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(parsed) != 3 {
		t.Fatalf("expected 2 reports plus summary, got %d lines:\n%s", len(parsed), stdout)
	}
	for i := 0; i < 2; i++ {
		var report batchReport
		if err := json.Unmarshal([]byte(parsed[i]), &report); err != nil {
			t.Fatalf("batch line %d is not JSON: %v\n%s", i, err, parsed[i])
		}
		if !report.OK || report.RendererMode != "worker" || len(report.Commands) != 1 || !report.Commands[0].Worker {
			t.Fatalf("unexpected worker batch report: %#v", report)
		}
	}
}

func TestInspectCacheExplainAndCompatCommands(t *testing.T) {
	dir := t.TempDir()
	cifPath := filepath.Join(dir, "one.cif")
	if err := os.WriteFile(cifPath, []byte(oneAtomCIF), 0o644); err != nil {
		t.Fatal(err)
	}
	jobPath := filepath.Join(dir, "job.json")
	output := filepath.Join(dir, "inspect.png")
	spec := map[string]any{
		"version": 1,
		"inputs": map[string]any{
			"input": map[string]any{"path": cifPath, "format": "mmcif"},
		},
		"scene": map[string]any{
			"structures": []any{map[string]any{
				"source": "input",
				"components": []any{map[string]any{
					"ref":            "all",
					"select":         "all",
					"representation": map[string]any{"type": "spacefill", "color": "#cc3399"},
				}},
			}},
			"camera": map[string]any{"focus": "all"},
		},
		"outputs": []any{map[string]any{"type": "image", "path": output, "size": []int{64, 64}}},
	}
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jobPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	stdout, stderr, err := runAppForTest(ctx, "inspect", jobPath, "--select", "chain:A", "--json")
	if err != nil {
		t.Fatalf("inspect failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var inspectReport struct {
		OK         bool             `json:"ok"`
		Components []map[string]any `json:"components"`
		Semantic   map[string]any   `json:"semantic"`
	}
	if err := json.Unmarshal([]byte(stdout), &inspectReport); err != nil {
		t.Fatalf("inspect stdout is not JSON: %v\n%s", err, stdout)
	}
	if !inspectReport.OK || len(inspectReport.Components) < 2 {
		t.Fatalf("unexpected inspect report: %#v", inspectReport)
	}
	if inspectReport.Semantic["ok"] != true {
		t.Fatalf("semantic inspect did not succeed: %#v", inspectReport.Semantic)
	}
	if _, ok := inspectReport.Semantic["camera"].(map[string]any); !ok {
		t.Fatalf("semantic inspect missing camera: %#v", inspectReport.Semantic)
	}
	representations, ok := inspectReport.Semantic["representations"].([]any)
	if !ok || len(representations) == 0 {
		t.Fatalf("semantic inspect missing representations: %#v", inspectReport.Semantic)
	}
	componentRepresentations, ok := inspectReport.Semantic["component_representations"].(map[string]any)
	if !ok || componentRepresentations["all"] == nil {
		t.Fatalf("semantic inspect missing component representation mapping: %#v", inspectReport.Semantic)
	}
	structures, ok := inspectReport.Semantic["structures"].([]any)
	if !ok || len(structures) == 0 {
		t.Fatalf("semantic inspect missing structures: %#v", inspectReport.Semantic)
	}
	firstStructure, _ := structures[0].(map[string]any)
	if modelDetails, ok := firstStructure["model_details"].([]any); !ok || len(modelDetails) == 0 {
		t.Fatalf("semantic inspect missing model metadata: %#v", firstStructure)
	}

	stdout, stderr, err = runAppForTest(ctx, "cache", "explain", "1cbs", "--cache", filepath.Join(dir, "cache"), "--json")
	if err != nil {
		t.Fatalf("cache explain failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var cacheReport map[string]any
	if err := json.Unmarshal([]byte(stdout), &cacheReport); err != nil {
		t.Fatalf("cache explain stdout is not JSON: %v\n%s", err, stdout)
	}
	if cacheReport["cache_path"] == "" || cacheReport["offline_available"] != false {
		t.Fatalf("unexpected cache explain report: %#v", cacheReport)
	}

	compatDir := filepath.Join(dir, "compat")
	stdout, stderr, err = runAppForTest(ctx, "compat", "check", "--out-dir", compatDir, "--json")
	if err != nil {
		t.Fatalf("compat check failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var compatReport struct {
		OK        bool   `json:"ok"`
		OutputDir string `json:"output_dir"`
	}
	if err := json.Unmarshal([]byte(stdout), &compatReport); err != nil {
		t.Fatalf("compat stdout is not JSON: %v\n%s", err, stdout)
	}
	if !compatReport.OK || compatReport.OutputDir == "" {
		t.Fatalf("compat report is not ok: %s", stdout)
	}
	if err := requireNonEmptyFile(filepath.Join(compatDir, "one.cif")); err != nil {
		t.Fatal(err)
	}
}

func TestRenderRemoteCacheThenOffline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(oneAtomCIF))
	}))
	defer server.Close()
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	jobPath := filepath.Join(dir, "job.json")
	output1 := filepath.Join(dir, "network.png")
	output2 := filepath.Join(dir, "offline.png")
	writeURLJob := func(output string) {
		t.Helper()
		spec := map[string]any{
			"version": 1,
			"inputs": map[string]any{
				"input": map[string]any{"url": server.URL + "/model.cif", "format": "mmcif"},
			},
			"scene": map[string]any{
				"canvas": map[string]any{"background": "white"},
				"structures": []any{map[string]any{
					"source": "input",
					"components": []any{map[string]any{
						"ref":    "all",
						"select": "all",
						"representation": map[string]any{
							"type":  "spacefill",
							"color": "#cc3399",
						},
					}},
				}},
				"camera": map[string]any{"focus": "all"},
			},
			"outputs": []any{map[string]any{"type": "image", "path": output, "size": []int{96, 72}}},
		}
		data, err := json.Marshal(spec)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(jobPath, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	writeURLJob(output1)
	stdout, stderr, err := runAppForTest(ctx, "render", jobPath, "--cache", cacheDir, "--json")
	if err != nil {
		t.Fatalf("network cached render failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	writeURLJob(output2)
	server.Close()
	stdout, stderr, err = runAppForTest(ctx, "render", jobPath, "--cache", cacheDir, "--offline", "--json")
	if err != nil {
		t.Fatalf("offline cached render failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var report renderReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("offline render stdout is not JSON: %v\n%s", err, stdout)
	}
	if len(report.CachedInputs) != 1 || !report.CachedInputs[0].Cached {
		t.Fatalf("expected offline cache hit, got %#v", report.CachedInputs)
	}
	if len(report.OutputFiles) != 1 || !report.OutputFiles[0].Verified {
		t.Fatalf("unexpected offline output report: %#v", report.OutputFiles)
	}
}

func TestExamplesCommandShowsDemo(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stdout, stderr, err := runAppForTest(ctx, "examples", "--json")
	if err != nil {
		t.Fatalf("examples failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var report struct {
		OK       bool                `json:"ok"`
		Examples []exampleDefinition `json:"examples"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("examples stdout is not JSON: %v\n%s", err, stdout)
	}
	foundDemo := false
	for _, example := range report.Examples {
		if example.Name == "demo" && strings.Contains(example.Command, "render --demo") && !example.Network {
			foundDemo = true
		}
	}
	if !report.OK || !foundDemo {
		t.Fatalf("demo example missing: %#v", report)
	}
}

func TestJobExamplesCommandSupportsJSON(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stdout, stderr, err := runAppForTest(ctx, "job", "examples", "list", "--json")
	if err != nil {
		t.Fatalf("job examples list failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var list struct {
		OK       bool     `json:"ok"`
		Examples []string `json:"examples"`
	}
	if err := json.Unmarshal([]byte(stdout), &list); err != nil {
		t.Fatalf("job examples list stdout is not JSON: %v\n%s", err, stdout)
	}
	if !list.OK || len(list.Examples) == 0 || list.Examples[0] != "default" {
		t.Fatalf("unexpected job examples list: %#v", list)
	}

	stdout, stderr, err = runAppForTest(ctx, "job", "examples", "show", "ligand", "--json")
	if err != nil {
		t.Fatalf("job examples show failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	var shown struct {
		OK   bool    `json:"ok"`
		Name string  `json:"name"`
		Job  job.Job `json:"job"`
	}
	if err := json.Unmarshal([]byte(stdout), &shown); err != nil {
		t.Fatalf("job examples show stdout is not JSON: %v\n%s", err, stdout)
	}
	if !shown.OK || shown.Name != "ligand" || shown.Job.Version != 1 || shown.Job.Scene.Camera.Focus != "ligand" {
		t.Fatalf("unexpected job example: %#v", shown)
	}

	stdout, stderr, err = runAppForTest(ctx, "job", "examples", "show", "missing", "--json")
	if err == nil {
		t.Fatalf("expected missing job example to fail\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	var failure errorReport
	if jsonErr := json.Unmarshal([]byte(stdout), &failure); jsonErr != nil {
		t.Fatalf("missing example failure is not JSON: %v\n%s", jsonErr, stdout)
	}
	if failure.Error.Code != string(kindInvalidInput) {
		t.Fatalf("unexpected missing example failure: %#v", failure)
	}
}

func TestSceneCompileMVSXIncludesExplicitAssets(t *testing.T) {
	dir := t.TempDir()
	cifPath := filepath.Join(dir, "one.cif")
	if err := os.WriteFile(cifPath, []byte(oneAtomCIF), 0o644); err != nil {
		t.Fatal(err)
	}
	assetPath := filepath.Join(dir, "labels.tsv")
	if err := os.WriteFile(assetPath, []byte("id\tlabel\n1\tone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	jobPath := filepath.Join(dir, "job.json")
	outPath := filepath.Join(dir, "scene.mvsx")
	spec := map[string]any{
		"version": 1,
		"runtime": map[string]any{
			"allow_paths":       []string{dir},
			"max_archive_bytes": 1 << 20,
		},
		"inputs": map[string]any{
			"input": map[string]any{"path": cifPath, "format": "mmcif"},
		},
		"scene": map[string]any{
			"structures": []any{map[string]any{
				"source":     "input",
				"components": []any{map[string]any{"select": "all"}},
			}},
		},
		"outputs": []any{map[string]any{"type": "mvsx", "path": outPath}},
		"assets":  []any{map[string]any{"name": "annotations/labels.tsv", "path": assetPath}},
	}
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jobPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stdout, stderr, err := runAppForTest(ctx, "scene", "compile", jobPath, "--out", outPath, "--json")
	if err != nil {
		t.Fatalf("scene compile failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	reader, err := zip.OpenReader(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	found := map[string]string{}
	for _, file := range reader.File {
		opened, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(opened)
		_ = opened.Close()
		if err != nil {
			t.Fatal(err)
		}
		found[file.Name] = string(content)
	}
	if found["index.mvsj"] == "" || found["annotations/labels.tsv"] != "id\tlabel\n1\tone\n" {
		t.Fatalf("unexpected archive contents: %#v", found)
	}
}

func requireZipEntries(t *testing.T, path string, names ...string) {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	found := map[string]bool{}
	for _, file := range reader.File {
		found[file.Name] = true
	}
	for _, name := range names {
		if !found[name] {
			t.Fatalf("zip %s missing %s; found %#v", path, name, found)
		}
	}
}

func requireZipEntryContains(t *testing.T, path string, name string, needle string) {
	t.Helper()
	content := readZipEntry(t, path, name)
	if !strings.Contains(content, needle) {
		t.Fatalf("zip %s entry %s does not contain %q:\n%s", path, name, needle, content)
	}
}

func requireZipEntryNotContains(t *testing.T, path string, name string, needle string) {
	t.Helper()
	content := readZipEntry(t, path, name)
	if needle != "" && strings.Contains(content, needle) {
		t.Fatalf("zip %s entry %s unexpectedly contains %q:\n%s", path, name, needle, content)
	}
}

func readZipEntry(t *testing.T, path string, name string) string {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		opened, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(opened)
		_ = opened.Close()
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	t.Fatalf("zip %s missing %s", path, name)
	return ""
}

func writeFakeArtifactRuntime(t *testing.T, root string) {
	t.Helper()
	required := []string{
		filepath.Join(root, "scripts", "render-mvs.js"),
		filepath.Join(root, "scripts", "molstar-node-cli.js"),
		filepath.Join(root, "node_modules", "node", "bin", "node"),
		filepath.Join(root, "node_modules", ".bin", "mvs-render"),
		filepath.Join(root, "node_modules", ".bin", "mvs-validate"),
		filepath.Join(root, "bin", artifactMolstarBinaryName()),
	}
	for _, path := range required {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if strings.Contains(path, string(os.PathSeparator)+"bin"+string(os.PathSeparator)) || strings.Contains(path, string(os.PathSeparator)+".bin"+string(os.PathSeparator)) {
			mode = 0o755
		}
		if err := os.WriteFile(path, []byte("#!/usr/bin/env sh\nexit 0\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
}

func artifactMolstarBinaryName() string {
	if runtime.GOOS == "windows" {
		return "molstar.exe"
	}
	return "molstar"
}

func writeZipForTest(t *testing.T, archivePath string, sourceDir string, prefix string) {
	t.Helper()
	out, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(out)
	walkErr := filepath.WalkDir(sourceDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(filepath.Join(prefix, rel))
		header.Method = zip.Deflate
		header.SetMode(info.Mode())
		file, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = file.Write(data)
		return err
	})
	if walkErr != nil {
		_ = writer.Close()
		_ = out.Close()
		t.Fatal(walkErr)
	}
	if err := writer.Close(); err != nil {
		_ = out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func runAppForTest(ctx context.Context, args ...string) (string, string, error) {
	restoreEnv := configureRuntimeForCLITest()
	defer restoreEnv()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	a := app{stdin: strings.NewReader(""), stdout: &stdout, stderr: &stderr, args: args}
	root := a.rootCommand()
	root.SetArgs(args)
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	err := root.ExecuteContext(ctx)
	return stdout.String(), stderr.String(), err
}

var testRuntimeConfigOnce sync.Once
var testRuntimeConfigPath string
var testRuntimeConfigErr error

func configureRuntimeForCLITest() func() {
	restore := []func(){}
	if value, ok := os.LookupEnv(render.ConfigEnv); !ok || strings.TrimSpace(value) == "" {
		path, err := defaultRuntimeConfigForCLITest()
		if err == nil {
			restore = append(restore, setEnvForCLITest(render.ConfigEnv, path))
		}
	}
	for _, key := range []string{"MOLSTAR_RENDER", "MOLSTAR_RENDER_FALLBACK", "MOLSTAR_VALIDATE"} {
		if _, ok := os.LookupEnv(key); !ok {
			restore = append(restore, setEnvForCLITest(key, ""))
		}
	}
	return func() {
		for i := len(restore) - 1; i >= 0; i-- {
			restore[i]()
		}
	}
}

func setEnvForCLITest(key string, value string) func() {
	old, hadOld := os.LookupEnv(key)
	_ = os.Setenv(key, value)
	return func() {
		if hadOld {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	}
}

func defaultRuntimeConfigForCLITest() (string, error) {
	testRuntimeConfigOnce.Do(func() {
		_, file, _, ok := runtime.Caller(0)
		if !ok {
			testRuntimeConfigErr = fmt.Errorf("runtime.Caller failed")
			return
		}
		root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
		if err := validateInstallHome(root); err != nil {
			testRuntimeConfigErr = err
			return
		}
		dir := filepath.Join(os.TempDir(), "headlessmolstar-cli-tests")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			testRuntimeConfigErr = err
			return
		}
		testRuntimeConfigPath = filepath.Join(dir, "config.json")
		testRuntimeConfigErr = render.WriteRuntimeConfig(testRuntimeConfigPath, render.RuntimeConfigForHome(root))
	})
	return testRuntimeConfigPath, testRuntimeConfigErr
}

func runInstalledForTest(ctx context.Context, binary string, configPath string, args ...string) (string, string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = os.TempDir()
	cmd.Env = append(os.Environ(),
		render.ConfigEnv+"="+configPath,
		"MOLSTAR_RENDER=",
		"MOLSTAR_RENDER_FALLBACK=",
		"MOLSTAR_VALIDATE=",
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func cliRepoRootForTest(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root not found from %s: %v", file, err)
	}
	return root
}

func chdirForCLITest(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(previous)
	})
}
