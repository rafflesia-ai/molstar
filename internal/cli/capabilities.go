package cli

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/molstar/internal/render"
)

type capabilitiesFlags struct {
	jsonReport  bool
	probeWorker bool
	timeout     time.Duration
}

type capabilitiesReport struct {
	OK       bool                       `json:"ok"`
	Config   doctorConfigReport         `json:"config"`
	Runtime  capabilitiesRuntimeReport  `json:"runtime"`
	Renderer capabilitiesRendererReport `json:"renderer"`
	Worker   capabilitiesWorkerReport   `json:"worker"`
	Matrix   []capabilityMatrixRow      `json:"matrix"`
}

type capabilitiesRuntimeReport struct {
	GOOS       string `json:"goos"`
	GOARCH     string `json:"goarch"`
	Executable string `json:"executable,omitempty"`
	Node       string `json:"node,omitempty"`
	Molstar    string `json:"molstar,omitempty"`
}

type capabilitiesRendererReport struct {
	Primary  capabilitiesCommandReport `json:"primary"`
	Fallback capabilitiesCommandReport `json:"fallback"`
	Validate capabilitiesCommandReport `json:"validate"`
}

type capabilitiesCommandReport struct {
	Command              []string                   `json:"command,omitempty"`
	Kind                 string                     `json:"kind"`
	Available            bool                       `json:"available"`
	SupportsCapabilities bool                       `json:"supports_capabilities"`
	Capabilities         *render.CapabilitiesReport `json:"capabilities,omitempty"`
	Error                string                     `json:"error,omitempty"`
}

type capabilitiesWorkerReport struct {
	Supported    bool           `json:"supported"`
	Probed       bool           `json:"probed"`
	OK           bool           `json:"ok,omitempty"`
	Command      []string       `json:"command,omitempty"`
	Capabilities map[string]any `json:"capabilities,omitempty"`
	Error        string         `json:"error,omitempty"`
}

type capabilityMatrixRow struct {
	Target               string   `json:"target"`
	Kind                 string   `json:"kind"`
	Command              []string `json:"command,omitempty"`
	Available            bool     `json:"available"`
	SupportsCapabilities bool     `json:"supports_capabilities,omitempty"`
	SupportsWorker       bool     `json:"supports_worker,omitempty"`
	WebGL                string   `json:"webgl,omitempty"`
	Canvas               string   `json:"canvas,omitempty"`
	SoftwareGL           string   `json:"software_gl,omitempty"`
	OK                   bool     `json:"ok"`
	Error                string   `json:"error,omitempty"`
}

func (a app) capabilitiesCommand() *cobra.Command {
	flags := &capabilitiesFlags{timeout: 20 * time.Second}
	cmd := &cobra.Command{
		Use:   "capabilities",
		Short: "Report renderer, Mol*, Node, canvas, GL, and worker capabilities",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("capabilities", flags.jsonReport, func() error {
				if len(args) != 0 {
					return markError(kindInvalidInput, fmt.Errorf("capabilities does not accept positional arguments"))
				}
				report := a.buildCapabilitiesReport(cmd.Context(), flags)
				if flags.jsonReport {
					if err := writeJSON(a.stdout, report); err != nil {
						return markError(kindInternal, err)
					}
					if !report.OK {
						return alreadyReported(markError(kindDoctor, fmt.Errorf("capabilities check failed")))
					}
					return nil
				}
				a.printCapabilitiesReport(report)
				if !report.OK {
					return markError(kindDoctor, fmt.Errorf("capabilities check failed"))
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&flags.jsonReport, "json", false, "write a machine-readable report to stdout")
	cmd.Flags().BoolVar(&flags.probeWorker, "probe-worker", false, "start one renderer worker and request its capabilities")
	cmd.Flags().DurationVar(&flags.timeout, "timeout", 20*time.Second, "capability probe timeout")
	return cmd
}

func (a app) buildCapabilitiesReport(parent context.Context, flags *capabilitiesFlags) capabilitiesReport {
	ctx, cancel := context.WithTimeout(parent, flags.timeout)
	defer cancel()

	runner := render.NewMolstar()
	primary := a.capabilityCommandReport(ctx, runner.RendererCommand)
	fallback := a.capabilityCommandReport(ctx, runner.RendererFallbackCommand)
	validate := capabilitiesCommandReport{
		Command:              append([]string{}, runner.ValidateCommand...),
		Kind:                 render.CommandKind(runner.ValidateCommand),
		Available:            render.Available(runner.ValidateCommand),
		SupportsCapabilities: false,
	}
	if !validate.Available {
		validate.Error = "command is not available"
	}

	worker := capabilitiesWorkerReport{
		Supported: render.SupportsWorker(runner.RendererCommand),
		Command:   append([]string{}, runner.RendererCommand...),
	}
	if flags.probeWorker && worker.Supported {
		worker.Probed = true
		pool, err := render.NewWorkerPool(1, runner.RendererCommand, nil, nil, nil)
		if err != nil {
			worker.Error = err.Error()
		} else {
			caps, err := pool.Capabilities(ctx)
			_ = pool.Close()
			if err != nil {
				worker.Error = err.Error()
			} else {
				worker.OK = true
				worker.Capabilities = caps
			}
		}
	}
	if flags.probeWorker && !worker.Supported {
		worker.Probed = true
		worker.Error = "primary renderer does not support worker protocol"
	}

	executable, _ := os.Executable()
	report := capabilitiesReport{
		Config: doctorConfig(),
		Runtime: capabilitiesRuntimeReport{
			GOOS:       runtime.GOOS,
			GOARCH:     runtime.GOARCH,
			Executable: executable,
		},
		Renderer: capabilitiesRendererReport{
			Primary:  primary,
			Fallback: fallback,
			Validate: validate,
		},
		Worker: worker,
	}
	report.Matrix = capabilitiesMatrix(primary, fallback, validate, worker)
	if primary.Capabilities != nil && primary.Capabilities.Renderer != nil {
		if value, ok := primary.Capabilities.Renderer["node"].(string); ok {
			report.Runtime.Node = value
		}
		if value, ok := primary.Capabilities.Renderer["molstar"].(string); ok {
			report.Runtime.Molstar = value
		}
	}
	report.OK = primary.Available && validate.Available
	if primary.SupportsCapabilities {
		report.OK = report.OK && primary.Capabilities != nil && primary.Capabilities.OK
	}
	if flags.probeWorker {
		report.OK = report.OK && worker.OK
	}
	return report
}

func (a app) capabilityCommandReport(ctx context.Context, command []string) capabilitiesCommandReport {
	report := capabilitiesCommandReport{
		Command:              append([]string{}, command...),
		Kind:                 render.CommandKind(command),
		Available:            render.Available(command),
		SupportsCapabilities: render.SupportsCapabilities(command),
	}
	if !report.Available {
		report.Error = "command is not available"
		return report
	}
	if report.SupportsCapabilities {
		runner := render.Molstar{RendererCommand: command}
		capabilities := runner.Capabilities(ctx)
		report.Capabilities = &capabilities
		if !capabilities.OK {
			report.Error = capabilities.Error
		}
	}
	return report
}

func (a app) printCapabilitiesReport(report capabilitiesReport) {
	status := "OK"
	if !report.OK {
		status = "FAIL"
	}
	fmt.Fprintf(a.stdout, "%s capabilities\n", status)
	fmt.Fprintf(a.stdout, "runtime %s/%s", report.Runtime.GOOS, report.Runtime.GOARCH)
	if report.Runtime.Node != "" {
		fmt.Fprintf(a.stdout, " node=%s", report.Runtime.Node)
	}
	if report.Runtime.Molstar != "" {
		fmt.Fprintf(a.stdout, " molstar=%s", report.Runtime.Molstar)
	}
	fmt.Fprintln(a.stdout)
	printCapabilityCommand(a.stdout, "primary", report.Renderer.Primary)
	printCapabilityCommand(a.stdout, "fallback", report.Renderer.Fallback)
	printCapabilityCommand(a.stdout, "validate", report.Renderer.Validate)
	fmt.Fprintln(a.stdout, "matrix")
	for _, row := range report.Matrix {
		fmt.Fprintf(a.stdout, "  %-9s %-20s available=%t ok=%t", row.Target, row.Kind, row.Available, row.OK)
		if row.WebGL != "" {
			fmt.Fprintf(a.stdout, " webgl=%s", row.WebGL)
		}
		if row.Canvas != "" {
			fmt.Fprintf(a.stdout, " canvas=%s", row.Canvas)
		}
		if row.SoftwareGL != "" {
			fmt.Fprintf(a.stdout, " software_gl=%s", row.SoftwareGL)
		}
		if row.Error != "" {
			fmt.Fprintf(a.stdout, " error=%s", singleLine(row.Error))
		}
		fmt.Fprintln(a.stdout)
	}
	worker := "unsupported"
	if report.Worker.Supported {
		worker = "supported"
	}
	if report.Worker.Probed {
		if report.Worker.OK {
			worker += ", probed"
		} else {
			worker += ", probe failed"
		}
	}
	fmt.Fprintf(a.stdout, "worker   %s\n", worker)
}

func capabilitiesMatrix(primary, fallback, validate capabilitiesCommandReport, worker capabilitiesWorkerReport) []capabilityMatrixRow {
	rows := []capabilityMatrixRow{
		capabilityCommandMatrixRow("primary", primary),
		capabilityCommandMatrixRow("fallback", fallback),
		capabilityCommandMatrixRow("validate", validate),
		{
			Target:         "worker",
			Kind:           render.CommandKind(worker.Command),
			Command:        append([]string{}, worker.Command...),
			Available:      worker.Supported,
			SupportsWorker: worker.Supported,
			OK:             !worker.Probed || worker.OK,
			Error:          worker.Error,
		},
	}
	if worker.Capabilities != nil {
		rows[len(rows)-1].WebGL = availabilityLabelFromMap(worker.Capabilities, "gl")
		rows[len(rows)-1].Canvas = availabilityLabelFromMap(worker.Capabilities, "canvas")
	}
	return rows
}

func capabilityCommandMatrixRow(target string, report capabilitiesCommandReport) capabilityMatrixRow {
	row := capabilityMatrixRow{
		Target:               target,
		Kind:                 report.Kind,
		Command:              append([]string{}, report.Command...),
		Available:            report.Available,
		SupportsCapabilities: report.SupportsCapabilities,
		SupportsWorker:       render.SupportsWorker(report.Command),
		OK:                   report.Available,
		Error:                report.Error,
	}
	if report.SupportsCapabilities {
		row.OK = report.Capabilities != nil && report.Capabilities.OK
	}
	if report.Capabilities != nil && report.Capabilities.Renderer != nil {
		row.WebGL = availabilityLabelFromMap(report.Capabilities.Renderer, "gl")
		row.Canvas = availabilityLabelFromMap(report.Capabilities.Renderer, "canvas")
		if software := os.Getenv("LIBGL_ALWAYS_SOFTWARE"); software != "" {
			row.SoftwareGL = software
		}
	}
	return row
}

func availabilityLabelFromMap(parent map[string]any, key string) string {
	value, ok := parent[key].(map[string]any)
	if !ok {
		return ""
	}
	available, _ := value["available"].(bool)
	if available {
		return "available"
	}
	if errText, _ := value["error"].(string); errText != "" {
		return "unavailable: " + errText
	}
	return "unavailable"
}

func printCapabilityCommand(w interface {
	Write([]byte) (int, error)
}, name string, report capabilitiesCommandReport) {
	status := "MISS"
	if report.Available {
		status = "OK"
	}
	detail := strings.Join(report.Command, " ")
	if detail == "" {
		detail = "not configured"
	}
	if report.SupportsCapabilities && report.Capabilities != nil && report.Capabilities.OK {
		fmt.Fprintf(w, "%-8s %s %s (capabilities protocol)\n", name, status, report.Kind)
		return
	}
	fmt.Fprintf(w, "%-8s %s %s (%s)\n", name, status, report.Kind, detail)
}
