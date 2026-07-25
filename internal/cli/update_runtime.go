package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sacha-ichbiah/molstar/internal/render"
)

type updateRuntimeFlags struct {
	home        string
	configPath  string
	skipInstall bool
	jsonReport  bool
	timeout     time.Duration
}

type updateRuntimeReport struct {
	OK           bool                       `json:"ok"`
	Home         string                     `json:"home"`
	Config       string                     `json:"config"`
	Installed    bool                       `json:"installed"`
	Install      []string                   `json:"install_command,omitempty"`
	Renderer     string                     `json:"renderer"`
	Validator    string                     `json:"validator"`
	Capabilities *render.CapabilitiesReport `json:"capabilities,omitempty"`
}

func (a app) updateRuntimeCommand() *cobra.Command {
	flags := &updateRuntimeFlags{timeout: 30 * time.Second}
	cmd := &cobra.Command{
		Use:   "update-runtime",
		Short: "Refresh or repair the packaged Node/Mol* renderer runtime",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("update-runtime", flags.jsonReport, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("update-runtime does not accept positional arguments"))
				}
				report, err := a.runUpdateRuntime(cmd.Context(), flags)
				if flags.jsonReport {
					if writeErr := writeJSON(a.stdout, report); writeErr != nil {
						return markError(kindInternal, writeErr)
					}
					if err != nil {
						return alreadyReported(err)
					}
					return nil
				}
				fmt.Fprintf(a.stdout, "runtime %s\n", report.Home)
				if report.Installed {
					fmt.Fprintf(a.stdout, "installed deps with %s\n", strings.Join(report.Install, " "))
				}
				fmt.Fprintf(a.stdout, "config %s\n", report.Config)
				if err != nil {
					return err
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&flags.home, "home", "", "runtime root; defaults to configured runtime home")
	cmd.Flags().StringVar(&flags.configPath, "config", "", "config path to write; defaults to current/default config")
	cmd.Flags().BoolVar(&flags.skipInstall, "skip-install", false, "only rewrite config and validate existing runtime files")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write a machine-readable report")
	cmd.Flags().DurationVar(&flags.timeout, "timeout", 30*time.Second, "capability probe timeout")
	return cmd
}

func (a app) runUpdateRuntime(ctx context.Context, flags *updateRuntimeFlags) (updateRuntimeReport, error) {
	home, err := detectRuntimeHome(flags.home)
	if err != nil {
		return updateRuntimeReport{OK: false}, markError(kindInvalidInput, err)
	}
	report := updateRuntimeReport{OK: true, Home: home}
	var installCommand []string
	if !flags.skipInstall {
		installCommand, err = npmInstallCommand(home)
		if err != nil {
			return report, markError(kindRuntime, err)
		}
		cmd := exec.CommandContext(ctx, installCommand[0], installCommand[1:]...)
		cmd.Dir = home
		output, installErr := cmd.CombinedOutput()
		if installErr != nil {
			report.OK = false
			return report, markError(kindRuntime, fmt.Errorf("%s failed: %w: %s", strings.Join(installCommand, " "), installErr, strings.TrimSpace(string(output))))
		}
		report.Installed = true
		report.Install = installCommand
	}
	if err := validateArtifactRuntime(home); err != nil {
		report.OK = false
		return report, markError(kindRuntime, err)
	}
	configPath := strings.TrimSpace(flags.configPath)
	if configPath == "" {
		configPath, err = render.DefaultConfigPath()
		if err != nil {
			report.OK = false
			return report, markError(kindInvalidInput, err)
		}
	}
	config := render.RuntimeConfigForHome(home)
	if err := render.WriteRuntimeConfig(configPath, config); err != nil {
		report.OK = false
		return report, markError(kindRender, err)
	}
	report.Config = configPath
	report.Renderer = strings.Join(config.RendererCommand, " ")
	report.Validator = strings.Join(config.ValidateCommand, " ")

	probeCtx, cancel := context.WithTimeout(ctx, flags.timeout)
	defer cancel()
	runner := render.NewMolstar()
	runner.RendererCommand = config.RendererCommand
	runner.RendererFallbackCommand = config.RendererFallbackCommand
	runner.ValidateCommand = config.ValidateCommand
	capabilities := runner.Capabilities(probeCtx)
	report.Capabilities = &capabilities
	if !capabilities.OK {
		report.OK = false
		return report, markError(kindDoctor, fmt.Errorf("runtime capability probe failed: %s", capabilities.Error))
	}
	return report, nil
}

func detectRuntimeHome(value string) (string, error) {
	if strings.TrimSpace(value) != "" {
		return filepath.Abs(value)
	}
	if config, _, err := render.LoadRuntimeConfig(); err == nil && config.Home != "" {
		return filepath.Abs(config.Home)
	}
	if wd, err := os.Getwd(); err == nil {
		if home, ok := findUpdateRuntimeHome(wd); ok {
			return home, nil
		}
	}
	return "", fmt.Errorf("could not detect runtime home; pass --home")
}

func findUpdateRuntimeHome(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		if regularFile(filepath.Join(dir, "package.json")) && regularFile(filepath.Join(dir, "scripts", "render-mvs.js")) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func npmInstallCommand(home string) ([]string, error) {
	if !regularFile(filepath.Join(home, "package.json")) {
		return nil, fmt.Errorf("missing package.json under %s", home)
	}
	if regularFile(filepath.Join(home, "package-lock.json")) {
		return []string{"npm", "ci"}, nil
	}
	return []string{"npm", "install"}, nil
}
