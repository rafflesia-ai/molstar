package cli

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/molstar/internal/render"
)

type installArtifactFlags struct {
	artifact      string
	prefix        string
	binDir        string
	name          string
	configPath    string
	force         bool
	installDeps   bool
	verify        bool
	verifyTimeout time.Duration
	jsonReport    bool
}

func (a app) installArtifactCommand() *cobra.Command {
	flags := &installArtifactFlags{name: "molstar", installDeps: true, verify: true, verifyTimeout: 60 * time.Second}
	cmd := &cobra.Command{
		Use:   "install-artifact",
		Short: "Install a packaged headless Mol* artifact",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("install-artifact", flags.jsonReport, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("install-artifact does not accept positional arguments"))
				}
				report, err := a.runInstallArtifact(cmd.Context(), flags)
				if err != nil {
					return err
				}
				if flags.jsonReport {
					return writeJSON(a.stdout, report)
				}
				fmt.Fprintf(a.stdout, "installed %s\n", report.Binary)
				fmt.Fprintf(a.stdout, "config %s\n", report.Config)
				fmt.Fprintf(a.stdout, "runtime %s\n", report.Home)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&flags.artifact, "artifact", "", "artifact tar.gz, zip, or unpacked runtime directory")
	cmd.Flags().StringVar(&flags.prefix, "prefix", "", "runtime install directory; defaults to XDG_DATA_HOME/molstar/runtime")
	cmd.Flags().StringVar(&flags.binDir, "bin-dir", "", "directory to install the molstar binary into")
	cmd.Flags().StringVar(&flags.name, "name", "molstar", "installed executable name")
	cmd.Flags().StringVar(&flags.configPath, "config", "", "config path; defaults to XDG config or ~/.config/molstar/config.json")
	cmd.Flags().BoolVar(&flags.verify, "verify", true, "probe the installed runtime and fail if it cannot render")
	cmd.Flags().DurationVar(&flags.verifyTimeout, "verify-timeout", 60*time.Second, "timeout for the post-install capability probe")
	cmd.Flags().BoolVar(&flags.force, "force", false, "overwrite existing executable or runtime directory")
	cmd.Flags().BoolVar(&flags.installDeps, "install-deps", true, "run npm install when renderer dependencies are missing")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write a machine-readable report")
	return cmd
}

func (a app) runInstallArtifact(ctx context.Context, flags *installArtifactFlags) (installReport, error) {
	source := strings.TrimSpace(flags.artifact)
	if source == "" {
		return installReport{}, markError(kindInvalidInput, fmt.Errorf("--artifact is required"))
	}
	source, err := filepath.Abs(source)
	if err != nil {
		return installReport{}, markError(kindInvalidInput, err)
	}
	info, err := os.Stat(source)
	if err != nil {
		return installReport{}, markError(kindInvalidInput, err)
	}
	runtimeRoot := source
	// cleanupExtracted names a freshly extracted archive directory to remove if
	// the artifact turns out not to contain a valid runtime, so a corrupt or wrong
	// artifact does not leave partial content behind that blocks a retry with
	// "already exists; pass --force". It is cleared once extraction validates.
	var cleanupExtracted string
	if info.IsDir() {
		if strings.TrimSpace(flags.prefix) != "" {
			runtimeRoot, err = installArtifactDirectory(source, flags.prefix, flags.force)
			if err != nil {
				return installReport{}, markError(kindRuntime, err)
			}
		}
	} else {
		prefix := strings.TrimSpace(flags.prefix)
		if prefix == "" {
			prefix, err = defaultArtifactRuntimeDir()
			if err != nil {
				return installReport{}, markError(kindInvalidInput, err)
			}
		}
		runtimeRoot, err = installArtifactArchive(source, prefix, flags.force)
		if err != nil {
			return installReport{}, markError(kindRuntime, err)
		}
		cleanupExtracted = prefix
	}
	defer func() {
		if cleanupExtracted != "" {
			_ = os.RemoveAll(cleanupExtracted)
		}
	}()
	runtimeRoot, err = resolveArtifactRuntimeRoot(runtimeRoot)
	if err != nil {
		return installReport{}, markError(kindRuntime, err)
	}
	if flags.installDeps {
		if err := ensureArtifactRendererDeps(ctx, runtimeRoot); err != nil {
			return installReport{}, markError(kindRuntime, err)
		}
	}
	if err := validateArtifactRuntime(runtimeRoot); err != nil {
		return installReport{}, markError(kindRuntime, err)
	}
	cleanupExtracted = "" // extraction validated; keep it
	return a.finishArtifactInstall(runtimeRoot, source, flags)
}

func (a app) finishArtifactInstall(runtimeRoot, source string, flags *installArtifactFlags) (installReport, error) {
	sourceBinary, err := findArtifactBinaryNamed(runtimeRoot, "molstar")
	if err != nil {
		return installReport{}, markError(kindRuntime, err)
	}
	binDir, err := installBinDir(flags.binDir)
	if err != nil {
		return installReport{}, markError(kindInvalidInput, err)
	}
	if strings.TrimSpace(flags.name) == "" || strings.ContainsRune(flags.name, os.PathSeparator) {
		return installReport{}, markError(kindInvalidInput, fmt.Errorf("invalid executable name %q", flags.name))
	}
	target := filepath.Join(binDir, flags.name)
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return installReport{}, markError(kindInvalidInput, err)
	}
	overwritten, err := installRuntimeBinary(sourceBinary, target, flags.force)
	if err != nil {
		return installReport{}, markError(kindRender, err)
	}
	binaries := map[string]string{"molstar": target}
	configPath := strings.TrimSpace(flags.configPath)
	if configPath == "" {
		configPath, err = render.DefaultConfigPath()
		if err != nil {
			return installReport{}, markError(kindInvalidInput, err)
		}
	}
	config := render.RuntimeConfigForHome(runtimeRoot)
	if err := render.WriteRuntimeConfig(configPath, config); err != nil {
		return installReport{}, markError(kindRender, err)
	}
	report := installReport{
		OK:          true,
		Binary:      target,
		Binaries:    binaries,
		Config:      configPath,
		Home:        runtimeRoot,
		Source:      source,
		Renderer:    strings.Join(config.RendererCommand, " "),
		Validator:   strings.Join(config.ValidateCommand, " "),
		Overwritten: overwritten,
	}
	// Prove the installed runtime actually runs, the same way update-runtime
	// does. Without this, install reported ok for a runtime that doctor and
	// update-runtime both rejected.
	if flags.verify {
		capabilities, err := probeInstalledRuntime(runtimeRoot, config, flags.verifyTimeout)
		report.Capabilities = capabilities
		if err != nil {
			report.OK = false
			return report, err
		}
	}
	return report, nil
}

// probeInstalledRuntime runs the renderer's capability probe against a freshly
// installed runtime.
func probeInstalledRuntime(runtimeRoot string, config render.RuntimeConfig, timeout time.Duration) (*render.CapabilitiesReport, error) {
	runner := render.NewMolstar()
	runner.Stdout = nil
	runner.Stderr = nil
	runner.Quiet = true
	runner.RendererCommand = config.RendererCommand
	runner.WorkingDirectory = runtimeRoot
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	capabilities := runner.Capabilities(ctx)
	if !capabilities.OK {
		// The envelope carries only a message, so fold in the probe's own stderr:
		// "exit status 1" alone hides the actionable line, which is usually a
		// missing module or a Node ABI mismatch.
		detail := strings.TrimSpace(capabilities.Error)
		if stderr := strings.TrimSpace(capabilities.Command.Stderr); stderr != "" {
			detail = detail + ": " + firstMeaningfulLine(stderr)
		}
		return &capabilities, markError(kindDoctor, fmt.Errorf("installed runtime failed its capability probe: %s; re-run with --verify=false to install without checking, then investigate with `molstar doctor --json`", detail))
	}
	return &capabilities, nil
}

// firstMeaningfulLine picks the most informative line out of a Node stack trace:
// the error line if there is one, otherwise the first non-blank line. Node
// prefixes the offending source and a caret before it, and names the class —
// "SyntaxError:", "TypeError:" — so match the suffix rather than a bare "Error:".
func firstMeaningfulLine(text string) string {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if name, _, found := strings.Cut(trimmed, ": "); found && strings.HasSuffix(name, "Error") && !strings.ContainsAny(name, " \t") {
			return trimmed
		}
	}
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func installArtifactDirectory(source string, prefix string, force bool) (string, error) {
	target, err := filepath.Abs(prefix)
	if err != nil {
		return "", err
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return "", err
	}
	if source == target {
		return target, nil
	}
	if pathHasContent(target) {
		if !force {
			return "", fmt.Errorf("%s already exists; pass --force to overwrite", target)
		}
		if err := os.RemoveAll(target); err != nil {
			return "", err
		}
	}
	if err := copyTree(source, target); err != nil {
		return "", err
	}
	return target, nil
}

func installArtifactArchive(source string, prefix string, force bool) (string, error) {
	target, err := filepath.Abs(prefix)
	if err != nil {
		return "", err
	}
	if pathHasContent(target) {
		if !force {
			return "", fmt.Errorf("%s already exists; pass --force to overwrite", target)
		}
		if err := os.RemoveAll(target); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", err
	}
	switch {
	case strings.HasSuffix(strings.ToLower(source), ".zip"):
		err = extractZip(source, target)
	default:
		err = extractTarGz(source, target)
	}
	if err != nil {
		return "", err
	}
	return target, nil
}

func resolveArtifactRuntimeRoot(root string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if looksLikeArtifactRuntime(root) {
		return root, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	var candidates []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(root, entry.Name())
		if looksLikeArtifactRuntime(candidate) {
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	return "", fmt.Errorf("could not find artifact runtime root under %s", root)
}

func looksLikeArtifactRuntime(root string) bool {
	if !regularFile(filepath.Join(root, "scripts", "render-mvs.js")) {
		return false
	}
	if regularFile(filepath.Join(root, "package.json")) {
		return true
	}
	_, err := findArtifactBinaryNamed(root, "molstar")
	return err == nil
}

func validateArtifactRuntime(root string) error {
	required := []string{
		filepath.Join(root, "scripts", "render-mvs.js"),
		filepath.Join(root, "scripts", "molstar-node-cli.js"),
		filepath.Join(root, "node_modules", ".bin", "mvs-render"),
		filepath.Join(root, "node_modules", ".bin", "mvs-validate"),
	}
	for _, path := range required {
		if !regularFile(path) {
			return fmt.Errorf("missing artifact runtime dependency %s", path)
		}
	}
	if !regularFile(filepath.Join(root, "node_modules", "node", "bin", "node")) {
		if _, err := exec.LookPath("node"); err != nil {
			return fmt.Errorf("missing artifact runtime dependency %s and node is not on PATH", filepath.Join(root, "node_modules", "node", "bin", "node"))
		}
	}
	return nil
}

func ensureArtifactRendererDeps(ctx context.Context, root string) error {
	if validateArtifactRuntime(root) == nil {
		return nil
	}
	if !regularFile(filepath.Join(root, "package.json")) {
		return validateArtifactRuntime(root)
	}
	args := []string{"install"}
	if regularFile(filepath.Join(root, "package-lock.json")) {
		args = []string{"ci"}
	}
	args = append(args, "--legacy-peer-deps")
	cmd := exec.CommandContext(ctx, "npm", args...)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("npm %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return validateArtifactRuntime(root)
}

func findArtifactBinaryNamed(root string, name string) (string, error) {
	base := artifactExecutableName(name)
	candidates := []string{
		filepath.Join(root, "bin", base),
		filepath.Join(root, base),
	}
	for _, candidate := range candidates {
		if regularFile(candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("missing %s binary under %s", name, root)
}

func artifactExecutableName(name string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		return name + ".exe"
	}
	return name
}

func installRuntimeBinary(source string, target string, force bool) (bool, error) {
	overwritten := false
	if _, err := os.Stat(target); err == nil {
		if !force {
			return false, fmt.Errorf("%s already exists; pass --force to overwrite", target)
		}
		overwritten = true
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if err := copyFile(source, target, 0o755); err != nil {
		return false, err
	}
	return overwritten, nil
}

func defaultArtifactRuntimeDir() (string, error) {
	dataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataHome, "molstar", "runtime"), nil
}

func pathHasContent(path string) bool {
	entries, err := os.ReadDir(path)
	return err == nil && len(entries) > 0
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func copyTree(source string, target string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(target, 0o755)
		}
		dst := filepath.Join(target, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			return os.Symlink(link, dst)
		}
		if entry.IsDir() {
			return os.MkdirAll(dst, info.Mode().Perm())
		}
		return copyFile(path, dst, info.Mode().Perm())
	})
}

func extractTarGz(source string, target string) error {
	file, err := os.Open(source)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		cleanName, err := cleanArchivePath(header.Name)
		if err != nil {
			return fmt.Errorf("unsafe archive path %q", header.Name)
		}
		dst := filepath.Join(target, cleanName)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, os.FileMode(header.Mode).Perm()); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(header.Mode).Perm())
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, reader); err != nil {
				_ = out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if filepath.IsAbs(header.Linkname) {
				return fmt.Errorf("unsafe absolute symlink %q", header.Name)
			}
			// Reject relative symlink targets that escape the extraction root, so
			// a later entry cannot be written through the symlink. This mirrors
			// the check extractZip already performs.
			if _, err := cleanArchivePath(filepath.Join(filepath.Dir(cleanName), header.Linkname)); err != nil {
				return fmt.Errorf("unsafe symlink %q", header.Name)
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(header.Linkname, dst); err != nil {
				return err
			}
		}
	}
}

func extractZip(source string, target string) error {
	reader, err := zip.OpenReader(source)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		cleanName, err := cleanArchivePath(file.Name)
		if err != nil {
			return fmt.Errorf("unsafe archive path %q", file.Name)
		}
		dst := filepath.Join(target, cleanName)
		mode := file.FileInfo().Mode()
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(dst, mode.Perm()); err != nil {
				return err
			}
			continue
		}
		if mode&os.ModeSymlink != 0 {
			opened, err := file.Open()
			if err != nil {
				return err
			}
			linkBytes, readErr := io.ReadAll(opened)
			closeErr := opened.Close()
			if readErr != nil {
				return readErr
			}
			if closeErr != nil {
				return closeErr
			}
			linkName := string(linkBytes)
			if filepath.IsAbs(linkName) {
				return fmt.Errorf("unsafe absolute symlink %q", file.Name)
			}
			if _, err := cleanArchivePath(filepath.Join(filepath.Dir(cleanName), linkName)); err != nil {
				return fmt.Errorf("unsafe symlink %q", file.Name)
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(linkName, dst); err != nil {
				return err
			}
			continue
		}
		opened, err := file.Open()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			_ = opened.Close()
			return err
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode.Perm())
		if err != nil {
			_ = opened.Close()
			return err
		}
		if _, err := io.Copy(out, opened); err != nil {
			_ = opened.Close()
			_ = out.Close()
			return err
		}
		if err := opened.Close(); err != nil {
			_ = out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
	}
	return nil
}

func cleanArchivePath(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	cleanName := filepath.Clean(name)
	if cleanName == "." || cleanName == string(os.PathSeparator) || filepath.IsAbs(cleanName) {
		return "", fmt.Errorf("empty or absolute archive path")
	}
	if cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive path escapes target")
	}
	return cleanName, nil
}
