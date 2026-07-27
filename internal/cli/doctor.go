package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rafflesia-ai/molstar/internal/job"
	"github.com/rafflesia-ai/molstar/internal/mvs"
	"github.com/rafflesia-ai/molstar/internal/render"
)

type doctorCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	Code   string `json:"code,omitempty"`
	Error  string `json:"error,omitempty"`
}

type doctorFix struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

type doctorReport struct {
	OK       bool                 `json:"ok"`
	Config   doctorConfigReport   `json:"config"`
	Renderer doctorRendererReport `json:"renderer"`
	Checks   []doctorCheck        `json:"checks"`
	Fixes    []doctorFix          `json:"fixes,omitempty"`
}

type doctorConfigReport struct {
	Path   string `json:"path,omitempty"`
	Loaded bool   `json:"loaded"`
	Home   string `json:"home,omitempty"`
	Error  string `json:"error,omitempty"`
}

type doctorRendererReport struct {
	Primary  doctorCommandStatus `json:"primary"`
	Fallback doctorCommandStatus `json:"fallback"`
	Validate doctorCommandStatus `json:"validate"`
}

type doctorCommandStatus struct {
	Command      []string                  `json:"command,omitempty"`
	Detail       string                    `json:"detail,omitempty"`
	Available    bool                      `json:"available"`
	Tested       bool                      `json:"tested"`
	OK           bool                      `json:"ok"`
	Capabilities render.CapabilitiesReport `json:"capabilities,omitempty"`
	Code         string                    `json:"code,omitempty"`
	Error        string                    `json:"error,omitempty"`
}

func (a app) doctorCommand() *cobra.Command {
	var jsonReport bool
	var skipProbe bool
	var probeSize string
	var fix bool
	var home string
	var configPath string
	var cacheDir string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check local headless rendering prerequisites",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return markError(kindInvalidInput, fmt.Errorf("doctor does not accept positional arguments"))
			}
			var fixes []doctorFix
			if fix {
				var err error
				fixes, err = a.runDoctorFix(cmd.Context(), home, configPath, cacheDir)
				if err != nil && !jsonReport {
					return err
				}
				if strings.TrimSpace(configPath) != "" {
					_ = os.Setenv(render.ConfigEnv, strings.TrimSpace(configPath))
				}
			}
			runner := render.NewMolstar()
			configReport := doctorConfig()
			rendererReport := doctorRendererReport{
				Primary:  doctorCommand(runner.RendererCommand),
				Fallback: doctorCommand(runner.RendererFallbackCommand),
				Validate: doctorCommand(runner.ValidateCommand),
			}
			checks := []doctorCheck{
				pathCheck("node"),
				pathCheck("npm"),
				commandCheck("primary renderer command", runner.RendererCommand, true),
				commandCheck("fallback renderer command", runner.RendererFallbackCommand, false),
				commandCheck("mvs-validate command", runner.ValidateCommand, true),
			}
			if rendererReport.Primary.Available && render.SupportsCapabilities(runner.RendererCommand) {
				capabilities := runner.Capabilities(cmd.Context())
				rendererReport.Primary.Capabilities = capabilities
				check := doctorCheck{Name: "primary renderer capabilities", OK: capabilities.OK}
				if capabilities.OK {
					if version, ok := capabilities.Renderer["molstar"].(string); ok {
						check.Detail = "Mol* " + version
					}
					if glDetail := rendererGLDetail(capabilities.Renderer); glDetail != "" {
						if check.Detail != "" {
							check.Detail += ", " + glDetail
						} else {
							check.Detail = glDetail
						}
					}
				} else {
					check.Error = capabilities.Error
					check.Code = doctorErrorCode(nil, capabilities.Error)
					rendererReport.Primary.Code = check.Code
					rendererReport.Primary.Error = capabilities.Error
				}
				checks = append(checks, check)
			}
			checks = append(checks, dockerDaemonCheck(cmd.Context()))
			if !skipProbe {
				size, err := parseSize(probeSize)
				if err != nil {
					return markError(kindInvalidInput, err)
				}
				if rendererReport.Primary.Available {
					primary := runner
					primary.RendererFallbackCommand = nil
					primaryCheck := a.renderProbe(cmd.Context(), primary, size, "primary render probe")
					rendererReport.Primary.Tested = true
					rendererReport.Primary.OK = primaryCheck.OK
					rendererReport.Primary.Error = primaryCheck.Error
					rendererReport.Primary.Code = primaryCheck.Code
					checks = append(checks, primaryCheck)
				} else {
					checks = append(checks, doctorCheck{Name: "primary render probe", OK: false, Error: "primary renderer command is not available"})
				}
				if rendererReport.Fallback.Available {
					fallback := runner
					fallback.RendererCommand = runner.RendererFallbackCommand
					fallback.RendererFallbackCommand = nil
					fallbackCheck := a.renderProbe(cmd.Context(), fallback, size, "fallback render probe")
					rendererReport.Fallback.Tested = true
					rendererReport.Fallback.OK = fallbackCheck.OK
					rendererReport.Fallback.Error = fallbackCheck.Error
					rendererReport.Fallback.Code = fallbackCheck.Code
					checks = append(checks, fallbackCheck)
				}
			}
			report := doctorReport{OK: allChecksOK(checks) && allFixesOK(fixes), Config: configReport, Renderer: rendererReport, Checks: checks, Fixes: fixes}
			if jsonReport {
				if err := writeJSON(a.stdout, report); err != nil {
					return markError(kindInternal, err)
				}
				if !report.OK {
					return alreadyReported(markError(kindDoctor, fmt.Errorf("doctor failed")))
				}
				return nil
			}
			for _, check := range checks {
				status := "OK"
				if !check.OK {
					status = "MISS"
				}
				fmt.Fprintf(a.stdout, "%-5s %s", status, check.Name)
				if check.Detail != "" {
					fmt.Fprintf(a.stdout, " (%s)", check.Detail)
				}
				if check.Error != "" {
					fmt.Fprintf(a.stdout, " - %s", singleLine(check.Error))
				}
				fmt.Fprintln(a.stdout)
			}
			if configReport.Path != "" {
				if configReport.Loaded {
					fmt.Fprintf(a.stdout, "OK    config (%s)\n", configReport.Path)
				} else {
					fmt.Fprintf(a.stdout, "INFO  config not loaded (%s", configReport.Path)
					if configReport.Error != "" {
						fmt.Fprintf(a.stdout, " - %s", singleLine(configReport.Error))
					}
					fmt.Fprintln(a.stdout, ")")
				}
			}
			for _, fix := range fixes {
				status := "OK"
				if !fix.OK {
					status = "FAIL"
				}
				fmt.Fprintf(a.stdout, "%-5s fix %s", status, fix.Name)
				if fix.Detail != "" {
					fmt.Fprintf(a.stdout, " (%s)", fix.Detail)
				}
				if fix.Error != "" {
					fmt.Fprintf(a.stdout, " - %s", singleLine(fix.Error))
				}
				fmt.Fprintln(a.stdout)
			}
			if !report.OK {
				fmt.Fprintln(a.stderr, "repair renderer deps with: molstar doctor --fix")
				return markError(kindDoctor, fmt.Errorf("doctor failed"))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonReport, "json", false, "write a machine-readable report to stdout")
	cmd.Flags().BoolVar(&skipProbe, "skip-probe", false, "skip the local GL/canvas render probe")
	cmd.Flags().StringVar(&probeSize, "probe-size", "64x64", "render probe size as WIDTHxHEIGHT")
	cmd.Flags().BoolVar(&fix, "fix", false, "repair local renderer config and missing npm dependencies")
	cmd.Flags().StringVar(&home, "home", "", "source checkout/runtime root used by --fix")
	cmd.Flags().StringVar(&configPath, "config", "", "runtime config path written by --fix")
	cmd.Flags().StringVar(&cacheDir, "cache", "", "cache directory created by --fix")
	return cmd
}

func doctorConfig() doctorConfigReport {
	defaultPath, defaultErr := render.DefaultConfigPath()
	config, path, err := render.LoadRuntimeConfig()
	if err != nil {
		if path == "" {
			path = defaultPath
		}
		report := doctorConfigReport{Path: path, Loaded: false, Error: err.Error()}
		if defaultErr != nil {
			report.Error = defaultErr.Error()
		}
		return report
	}
	return doctorConfigReport{Path: path, Loaded: true, Home: config.Home}
}

func (a app) runDoctorFix(ctx context.Context, home string, configPath string, cacheDir string) ([]doctorFix, error) {
	var fixes []doctorFix
	resolvedHome, err := detectDoctorFixHome(home)
	if err != nil {
		fix := doctorFix{Name: "detect home", OK: false, Error: err.Error()}
		return append(fixes, fix), markError(kindDoctor, err)
	}
	fixes = append(fixes, doctorFix{Name: "detect home", OK: true, Detail: resolvedHome})

	if err := validateDoctorRuntimeHome(resolvedHome); err != nil {
		if !regularFile(filepath.Join(resolvedHome, "package.json")) {
			fix := doctorFix{Name: "npm install", OK: false, Error: err.Error()}
			return append(fixes, fix), markError(kindDoctor, err)
		}
		if installErr := ensureArtifactRendererDeps(ctx, resolvedHome); installErr != nil {
			fix := doctorFix{Name: "npm install", OK: false, Error: installErr.Error()}
			return append(fixes, fix), markError(kindDoctor, fmt.Errorf("npm install failed: %w", installErr))
		}
		fixes = append(fixes, doctorFix{Name: "npm install", OK: true, Detail: "renderer dependencies ready"})
	} else {
		fixes = append(fixes, doctorFix{Name: "npm install", OK: true, Detail: "dependencies already present"})
	}
	if err := validateDoctorRuntimeHome(resolvedHome); err != nil {
		fix := doctorFix{Name: "validate home", OK: false, Error: err.Error()}
		return append(fixes, fix), markError(kindDoctor, err)
	}
	fixes = append(fixes, doctorFix{Name: "validate home", OK: true})

	target := strings.TrimSpace(configPath)
	if target == "" {
		var err error
		target, err = render.DefaultConfigPath()
		if err != nil {
			fix := doctorFix{Name: "write config", OK: false, Error: err.Error()}
			return append(fixes, fix), markError(kindDoctor, err)
		}
	}
	if err := render.WriteRuntimeConfig(target, render.RuntimeConfigForHome(resolvedHome)); err != nil {
		fix := doctorFix{Name: "write config", OK: false, Error: err.Error()}
		return append(fixes, fix), markError(kindDoctor, err)
	}
	fixes = append(fixes, doctorFix{Name: "write config", OK: true, Detail: target})
	cacheTarget := strings.TrimSpace(cacheDir)
	if cacheTarget == "" {
		cacheTarget, err = defaultDoctorCacheDir()
		if err != nil {
			fix := doctorFix{Name: "cache dir", OK: false, Error: err.Error()}
			return append(fixes, fix), markError(kindDoctor, err)
		}
	}
	if err := os.MkdirAll(cacheTarget, 0o755); err != nil {
		fix := doctorFix{Name: "cache dir", OK: false, Error: err.Error()}
		return append(fixes, fix), markError(kindDoctor, err)
	}
	fixes = append(fixes, doctorFix{Name: "cache dir", OK: true, Detail: cacheTarget})
	fixes = append(fixes, dockerDaemonFix(ctx))
	return fixes, nil
}

func validateDoctorRuntimeHome(home string) error {
	if err := validateInstallHome(home); err == nil {
		return nil
	}
	return validateArtifactRuntime(home)
}

func defaultDoctorCacheDir() (string, error) {
	if value := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); value != "" {
		return filepath.Join(value, "molstar"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "molstar"), nil
}

func dockerDaemonCheck(ctx context.Context) doctorCheck {
	if _, err := exec.LookPath("docker"); err != nil {
		return doctorCheck{Name: "docker daemon (optional)", OK: true, Detail: "docker not installed"}
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(checkCtx, "docker", "info")
	output, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		if checkCtx.Err() != nil {
			detail = checkCtx.Err().Error()
		}
		return doctorCheck{Name: "docker daemon (optional)", OK: true, Detail: "not reachable", Error: detail}
	}
	return doctorCheck{Name: "docker daemon (optional)", OK: true, Detail: "reachable"}
}

func dockerDaemonFix(ctx context.Context) doctorFix {
	if _, err := exec.LookPath("docker"); err != nil {
		return doctorFix{Name: "docker daemon", OK: true, Detail: "docker not installed; install Docker Desktop to run docker:verify"}
	}
	check := dockerDaemonCheck(ctx)
	if check.Error == "" {
		return doctorFix{Name: "docker daemon", OK: true, Detail: "reachable"}
	}
	return doctorFix{Name: "docker daemon", OK: true, Detail: "not reachable; start or restart Docker Desktop, then run npm run docker:verify"}
}

func doctorCommand(command []string) doctorCommandStatus {
	available := render.Available(command)
	return doctorCommandStatus{
		Command:   append([]string{}, command...),
		Detail:    commandDetail(command),
		Available: available,
		OK:        available,
	}
}

func commandCheck(name string, command []string, required bool) doctorCheck {
	if len(command) == 0 {
		if required {
			return doctorCheck{Name: name, OK: false, Code: string(kindRenderer), Error: "not configured"}
		}
		return doctorCheck{Name: name, OK: true, Detail: "not configured"}
	}
	if !render.Available(command) {
		if required {
			return doctorCheck{Name: name, OK: false, Detail: commandDetail(command), Code: string(kindRenderer), Error: "command is not available"}
		}
		return doctorCheck{Name: name, OK: true, Detail: commandDetail(command), Error: "optional command is not available"}
	}
	return doctorCheck{Name: name, OK: true, Detail: commandDetail(command)}
}

func pathCheck(name string) doctorCheck {
	path, err := exec.LookPath(name)
	if err != nil {
		return doctorCheck{Name: name, OK: false, Code: string(kindRenderer), Error: err.Error()}
	}
	return doctorCheck{Name: name, OK: true, Detail: path}
}

func commandDetail(command []string) string {
	if len(command) == 0 {
		return ""
	}
	return strings.Join(command, " ")
}

func allChecksOK(checks []doctorCheck) bool {
	for _, check := range checks {
		if !check.OK {
			return false
		}
	}
	return true
}

func allFixesOK(fixes []doctorFix) bool {
	for _, fix := range fixes {
		if !fix.OK {
			return false
		}
	}
	return true
}

func detectDoctorFixHome(value string) (string, error) {
	if strings.TrimSpace(value) != "" {
		return filepath.Abs(value)
	}
	if wd, err := os.Getwd(); err == nil {
		if home, ok := findDoctorFixHome(wd); ok {
			return home, nil
		}
	}
	if exe, err := os.Executable(); err == nil {
		if home, ok := findDoctorFixHome(filepath.Dir(exe)); ok {
			return home, nil
		}
	}
	return "", fmt.Errorf("could not detect source home; pass --home")
}

func findDoctorFixHome(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		required := []string{
			filepath.Join(dir, "go.mod"),
			filepath.Join(dir, "cmd", "molstar", "main.go"),
			filepath.Join(dir, "scripts", "render-mvs.js"),
			filepath.Join(dir, "package.json"),
		}
		ok := true
		for _, path := range required {
			info, err := os.Stat(path)
			if err != nil || info.IsDir() {
				ok = false
				break
			}
		}
		if ok {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func (a app) renderProbe(ctx context.Context, runner render.Molstar, size []int, name string) doctorCheck {
	dir, err := os.MkdirTemp("", "headlessmolstar-doctor-*")
	if err != nil {
		return doctorCheck{Name: name, OK: false, Error: err.Error()}
	}
	defer os.RemoveAll(dir)

	cifPath := filepath.Join(dir, "one.cif")
	if err := os.WriteFile(cifPath, []byte(oneAtomCIF), 0o644); err != nil {
		return doctorCheck{Name: name, OK: false, Error: err.Error()}
	}
	imagePath := filepath.Join(dir, "probe.png")
	j := job.Job{
		Version: 1,
		Inputs: map[string]job.Input{
			"probe": {Path: cifPath, Format: "mmcif"},
		},
		Scene: job.Scene{
			Canvas: job.Canvas{Background: "white"},
			Structures: []job.Structure{{
				Source: "probe",
				Components: []job.Component{{
					Ref:    "atom",
					Select: "all",
					Representation: job.Representation{
						Type:  "spacefill",
						Color: "#cc3399",
					},
				}},
			}},
			Camera: job.Camera{Focus: "all"},
		},
		Outputs: []job.Output{{
			Type: "image",
			Path: imagePath,
			Size: size,
		}},
	}
	compiled, err := mvs.Compile(j)
	if err != nil {
		return doctorCheck{Name: name, OK: false, Error: err.Error()}
	}
	scenePath := filepath.Join(dir, "probe.mvsj")
	if err := mvs.WriteFile(scenePath, compiled.Document); err != nil {
		return doctorCheck{Name: name, OK: false, Error: err.Error()}
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner.Stdout = &stdout
	runner.Stderr = &stderr
	runner.Quiet = true
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := runner.RenderImage(ctx, render.ImageRequest{InputMVS: scenePath, Output: j.Outputs[0]}); err != nil {
		detail := stderr.String()
		if strings.TrimSpace(detail) == "" {
			detail = stdout.String()
		}
		if strings.TrimSpace(detail) == "" {
			detail = err.Error()
		}
		return doctorCheck{Name: name, OK: false, Code: doctorErrorCode(err, detail), Error: detail}
	}
	info, err := os.Stat(imagePath)
	if err != nil {
		return doctorCheck{Name: name, OK: false, Error: err.Error()}
	}
	if info.Size() == 0 {
		return doctorCheck{Name: name, OK: false, Error: "probe image is empty"}
	}
	return doctorCheck{Name: name, OK: true, Detail: fmt.Sprintf("%dx%d PNG, %d bytes", size[0], size[1], info.Size())}
}

func doctorErrorCode(err error, detail string) string {
	if strings.TrimSpace(detail) != "" {
		return string(classifyError(fmt.Errorf("%s", detail)))
	}
	if err != nil {
		return string(classifyError(err))
	}
	return string(kindInternal)
}

func singleLine(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	return strings.Join(fields, " ")
}

const oneAtomCIF = `data_one
_entry.id one
_cell.entry_id one
_cell.length_a 1
_cell.length_b 1
_cell.length_c 1
_cell.angle_alpha 90
_cell.angle_beta 90
_cell.angle_gamma 90
_symmetry.entry_id one
_symmetry.space_group_name_H-M 'P 1'
loop_
_atom_site.group_PDB
_atom_site.id
_atom_site.type_symbol
_atom_site.label_atom_id
_atom_site.label_alt_id
_atom_site.label_comp_id
_atom_site.label_asym_id
_atom_site.label_entity_id
_atom_site.label_seq_id
_atom_site.pdbx_PDB_ins_code
_atom_site.Cartn_x
_atom_site.Cartn_y
_atom_site.Cartn_z
_atom_site.occupancy
_atom_site.B_iso_or_equiv
_atom_site.pdbx_formal_charge
_atom_site.auth_seq_id
_atom_site.auth_comp_id
_atom_site.auth_asym_id
_atom_site.auth_atom_id
_atom_site.pdbx_PDB_model_num
ATOM 1 C C . GLY A 1 1 ? 0.0 0.0 0.0 1.00 10.00 ? 1 GLY A C 1
`
