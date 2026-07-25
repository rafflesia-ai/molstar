package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sacha-ichbiah/molstar/internal/render"
)

type installFlags struct {
	home       string
	binDir     string
	name       string
	configPath string
	force      bool
	jsonReport bool
}

type installReport struct {
	OK          bool              `json:"ok"`
	Binary      string            `json:"binary"`
	Binaries    map[string]string `json:"binaries,omitempty"`
	Config      string            `json:"config"`
	Home        string            `json:"home"`
	Source      string            `json:"source,omitempty"`
	Renderer    string            `json:"renderer"`
	Validator   string            `json:"validator"`
	Overwritten bool              `json:"overwritten,omitempty"`
}

func (a app) installLocalCommand() *cobra.Command {
	flags := &installFlags{name: "molstar"}
	cmd := &cobra.Command{
		Use:     "install-local",
		Aliases: []string{"install"},
		Short:   "Install this CLI and renderer config onto the local PATH",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("install-local", flags.jsonReport, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("install-local does not accept positional arguments"))
				}
				report, err := a.runInstallLocal(cmd.Context(), flags)
				if err != nil {
					return err
				}
				if flags.jsonReport {
					return writeJSON(a.stdout, report)
				}
				fmt.Fprintf(a.stdout, "installed %s\n", report.Binary)
				fmt.Fprintf(a.stdout, "config %s\n", report.Config)
				fmt.Fprintf(a.stdout, "home %s\n", report.Home)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&flags.home, "home", "", "source checkout/runtime root; auto-detected when omitted")
	cmd.Flags().StringVar(&flags.binDir, "bin-dir", "", "directory to install the molstar binary into")
	cmd.Flags().StringVar(&flags.name, "name", "molstar", "installed executable name")
	cmd.Flags().StringVar(&flags.configPath, "config", "", "config path; defaults to XDG config or ~/.config/molstar/config.json")
	cmd.Flags().BoolVar(&flags.force, "force", false, "overwrite an existing executable")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write a machine-readable report")
	return cmd
}

func (a app) runInstallLocal(ctx context.Context, flags *installFlags) (installReport, error) {
	home, err := detectInstallHome(flags.home)
	if err != nil {
		return installReport{}, markError(kindInvalidInput, err)
	}
	if err := validateInstallHome(home); err != nil {
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
	overwritten := false
	if _, err := os.Stat(target); err == nil {
		if !flags.force {
			return installReport{}, markError(kindInvalidInput, fmt.Errorf("%s already exists; pass --force to overwrite", target))
		}
		overwritten = true
	} else if !os.IsNotExist(err) {
		return installReport{}, markError(kindInvalidInput, err)
	}

	tmpDir, err := os.MkdirTemp("", "headlessmolstar-install-*")
	if err != nil {
		return installReport{}, markError(kindInternal, err)
	}
	defer os.RemoveAll(tmpDir)
	tmpBinary := filepath.Join(tmpDir, flags.name)
	buildArgs := []string{"build", "-o", tmpBinary}
	if ldflags := localBuildLDFlags(home); ldflags != "" {
		buildArgs = append(buildArgs, "-ldflags", ldflags)
	}
	buildArgs = append(buildArgs, "./cmd/molstar")
	build := exec.CommandContext(ctx, "go", buildArgs...)
	build.Dir = home
	buildOutput, err := build.CombinedOutput()
	if err != nil {
		return installReport{}, markError(kindRuntime, fmt.Errorf("go build failed: %w: %s", err, strings.TrimSpace(string(buildOutput))))
	}

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return installReport{}, markError(kindInvalidInput, err)
	}
	if err := copyFile(tmpBinary, target, 0o755); err != nil {
		return installReport{}, markError(kindRender, err)
	}

	configPath := strings.TrimSpace(flags.configPath)
	if configPath == "" {
		configPath, err = render.DefaultConfigPath()
		if err != nil {
			return installReport{}, markError(kindInvalidInput, err)
		}
	}
	config := render.RuntimeConfigForHome(home)
	if err := render.WriteRuntimeConfig(configPath, config); err != nil {
		return installReport{}, markError(kindRender, err)
	}
	return installReport{
		OK:          true,
		Binary:      target,
		Config:      configPath,
		Home:        home,
		Renderer:    strings.Join(config.RendererCommand, " "),
		Validator:   strings.Join(config.ValidateCommand, " "),
		Overwritten: overwritten,
	}, nil
}

func localBuildLDFlags(home string) string {
	const pkg = "github.com/sacha-ichbiah/molstar/internal/cli"
	version := packageVersion(home)
	if version == "" {
		version = "dev"
	}
	commit := gitOutput(home, "rev-parse", "--short=12", "HEAD")
	date := time.Now().UTC().Format(time.RFC3339)
	return strings.Join([]string{
		"-X", pkg + ".buildVersion=" + version,
		"-X", pkg + ".buildCommit=" + commit,
		"-X", pkg + ".buildDate=" + date,
	}, " ")
}

func packageVersion(home string) string {
	data, err := os.ReadFile(filepath.Join(home, "package.json"))
	if err != nil {
		return ""
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return ""
	}
	return strings.TrimSpace(pkg.Version)
}

func gitOutput(home string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = home
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func detectInstallHome(value string) (string, error) {
	if strings.TrimSpace(value) != "" {
		return filepath.Abs(value)
	}
	if config, _, err := render.LoadRuntimeConfig(); err == nil && config.Home != "" {
		if validateInstallHome(config.Home) == nil {
			return filepath.Abs(config.Home)
		}
	}
	if wd, err := os.Getwd(); err == nil {
		if home, ok := findInstallHome(wd); ok {
			return home, nil
		}
	}
	if exe, err := os.Executable(); err == nil {
		if home, ok := findInstallHome(filepath.Dir(exe)); ok {
			return home, nil
		}
	}
	return "", fmt.Errorf("could not detect source home; pass --home")
}

func findInstallHome(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		if validateInstallHome(dir) == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func validateInstallHome(home string) error {
	required := []string{
		filepath.Join(home, "go.mod"),
		filepath.Join(home, "cmd", "molstar", "main.go"),
		filepath.Join(home, "scripts", "render-mvs.js"),
		filepath.Join(home, "scripts", "molstar-node-cli.js"),
		filepath.Join(home, "node_modules", "node", "bin", "node"),
		filepath.Join(home, "node_modules", ".bin", "mvs-render"),
		filepath.Join(home, "node_modules", ".bin", "mvs-validate"),
	}
	for _, path := range required {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("missing install dependency %s", path)
		}
		if info.IsDir() {
			return fmt.Errorf("install dependency is a directory: %s", path)
		}
	}
	return nil
}

func installBinDir(value string) (string, error) {
	if strings.TrimSpace(value) != "" {
		return filepath.Abs(value)
	}
	candidates := []string{"/opt/homebrew/bin", "/usr/local/bin"}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".local", "bin"))
	}
	for _, dir := range candidates {
		if isWritableDir(dir) {
			return dir, nil
		}
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if isWritableDir(dir) {
			return dir, nil
		}
	}
	return "", fmt.Errorf("could not find a writable bin directory; pass --bin-dir")
}

func isWritableDir(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	file, err := os.CreateTemp(dir, ".headlessmolstar-write-*")
	if err != nil {
		return false
	}
	name := file.Name()
	_ = file.Close()
	_ = os.Remove(name)
	return true
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}
