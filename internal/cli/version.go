package cli

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/molstar/internal/render"
)

var (
	buildVersion = "dev"
	buildCommit  = ""
	buildDate    = ""
)

type versionFlags struct {
	jsonReport  bool
	skipRuntime bool
	timeout     time.Duration
}

type versionReport struct {
	OK      bool                  `json:"ok"`
	CLI     cliVersionReport      `json:"cli"`
	Config  doctorConfigReport    `json:"config"`
	Runtime *runtimeVersionReport `json:"runtime,omitempty"`
}

type cliVersionReport struct {
	Version    string `json:"version"`
	Commit     string `json:"commit,omitempty"`
	Date       string `json:"date,omitempty"`
	GoVersion  string `json:"go_version"`
	GOOS       string `json:"goos"`
	GOARCH     string `json:"goarch"`
	Executable string `json:"executable,omitempty"`
	Module     string `json:"module,omitempty"`
}

type runtimeVersionReport struct {
	OK       bool                      `json:"ok"`
	Node     string                    `json:"node,omitempty"`
	Molstar  string                    `json:"molstar,omitempty"`
	Renderer string                    `json:"renderer,omitempty"`
	GL       map[string]any            `json:"gl,omitempty"`
	Canvas   map[string]any            `json:"canvas,omitempty"`
	Error    string                    `json:"error,omitempty"`
	Probe    render.CapabilitiesReport `json:"probe,omitempty"`
}

func (a app) versionCommand() *cobra.Command {
	flags := &versionFlags{timeout: 20 * time.Second}
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Report CLI build and renderer runtime provenance",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("version", flags.jsonReport, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("version does not accept positional arguments"))
				}
				report := buildVersionReport(cmd.Context(), flags)
				if flags.jsonReport {
					return writeJSON(a.stdout, report)
				}
				fmt.Fprintf(a.stdout, "molstar %s", report.CLI.Version)
				if report.CLI.Commit != "" {
					fmt.Fprintf(a.stdout, " (%s)", report.CLI.Commit)
				}
				fmt.Fprintln(a.stdout)
				fmt.Fprintf(a.stdout, "go %s %s/%s\n", report.CLI.GoVersion, report.CLI.GOOS, report.CLI.GOARCH)
				if report.Runtime != nil && report.Runtime.OK {
					fmt.Fprintf(a.stdout, "runtime node=%s molstar=%s\n", report.Runtime.Node, report.Runtime.Molstar)
				} else if report.Runtime != nil && report.Runtime.Error != "" {
					fmt.Fprintf(a.stdout, "runtime unavailable: %s\n", singleLine(report.Runtime.Error))
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write a machine-readable report to stdout")
	cmd.Flags().BoolVar(&flags.skipRuntime, "skip-runtime", false, "skip Node/Mol* runtime probing")
	cmd.Flags().DurationVar(&flags.timeout, "timeout", 20*time.Second, "runtime probe timeout")
	return cmd
}

func buildVersionReport(parent context.Context, flags *versionFlags) versionReport {
	executable, _ := os.Executable()
	cli := cliVersionReport{
		Version:    buildVersion,
		Commit:     buildCommit,
		Date:       buildDate,
		GoVersion:  runtime.Version(),
		GOOS:       runtime.GOOS,
		GOARCH:     runtime.GOARCH,
		Executable: executable,
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		cli.Module = info.Main.Path
		if cli.Version == "" || cli.Version == "dev" {
			cli.Version = info.Main.Version
			if cli.Version == "(devel)" {
				cli.Version = "dev"
			}
		}
		if cli.Commit == "" || cli.Date == "" {
			for _, setting := range info.Settings {
				switch setting.Key {
				case "vcs.revision":
					if cli.Commit == "" {
						cli.Commit = setting.Value
					}
				case "vcs.time":
					if cli.Date == "" {
						cli.Date = setting.Value
					}
				}
			}
		}
	}
	if cli.Version == "" {
		cli.Version = "dev"
	}
	report := versionReport{
		OK:     true,
		CLI:    cli,
		Config: doctorConfig(),
	}
	if flags.skipRuntime {
		return report
	}
	ctx, cancel := context.WithTimeout(parent, flags.timeout)
	defer cancel()
	runner := render.NewMolstar()
	probe := runner.Capabilities(ctx)
	runtimeReport := runtimeVersionReport{OK: probe.OK, Probe: probe}
	if probe.Renderer != nil {
		runtimeReport.Renderer, _ = probe.Renderer["protocol"].(string)
		runtimeReport.Node, _ = probe.Renderer["node"].(string)
		runtimeReport.Molstar, _ = probe.Renderer["molstar"].(string)
		runtimeReport.GL, _ = probe.Renderer["gl"].(map[string]any)
		runtimeReport.Canvas, _ = probe.Renderer["canvas"].(map[string]any)
	}
	if !probe.OK {
		runtimeReport.Error = probe.Error
	}
	report.Runtime = &runtimeReport
	return report
}
