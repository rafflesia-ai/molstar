package job

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Job struct {
	Version int              `json:"version" yaml:"version"`
	Runtime Runtime          `json:"runtime,omitempty" yaml:"runtime,omitempty"`
	Inputs  map[string]Input `json:"inputs" yaml:"inputs"`
	Scene   Scene            `json:"scene" yaml:"scene"`
	Outputs []Output         `json:"outputs,omitempty" yaml:"outputs,omitempty"`
	Assets  []ArchiveAsset   `json:"assets,omitempty" yaml:"assets,omitempty"`
}

type Runtime struct {
	Cache            string   `json:"cache,omitempty" yaml:"cache,omitempty"`
	Profile          string   `json:"profile,omitempty" yaml:"profile,omitempty"`
	Network          *bool    `json:"network,omitempty" yaml:"network,omitempty"`
	Offline          bool     `json:"offline,omitempty" yaml:"offline,omitempty"`
	Strict           bool     `json:"strict,omitempty" yaml:"strict,omitempty"`
	TimeoutSeconds   int      `json:"timeout_seconds,omitempty" yaml:"timeout_seconds,omitempty"`
	MaxPixels        int      `json:"max_pixels,omitempty" yaml:"max_pixels,omitempty"`
	MaxAtoms         int      `json:"max_atoms,omitempty" yaml:"max_atoms,omitempty"`
	MaxOutputs       int      `json:"max_outputs,omitempty" yaml:"max_outputs,omitempty"`
	MaxDownloadBytes int64    `json:"max_download_bytes,omitempty" yaml:"max_download_bytes,omitempty"`
	MaxArchiveBytes  int64    `json:"max_archive_bytes,omitempty" yaml:"max_archive_bytes,omitempty"`
	AllowHosts       []string `json:"allow_hosts,omitempty" yaml:"allow_hosts,omitempty"`
	AllowPaths       []string `json:"allow_paths,omitempty" yaml:"allow_paths,omitempty"`
}

type Input struct {
	ID       string `json:"id,omitempty" yaml:"id,omitempty"`
	Provider string `json:"provider,omitempty" yaml:"provider,omitempty"`
	Path     string `json:"path,omitempty" yaml:"path,omitempty"`
	URL      string `json:"url,omitempty" yaml:"url,omitempty"`
	Format   string `json:"format,omitempty" yaml:"format,omitempty"`
	Assembly string `json:"assembly,omitempty" yaml:"assembly,omitempty"`
}

type Scene struct {
	Canvas     Canvas      `json:"canvas,omitempty" yaml:"canvas,omitempty"`
	Structures []Structure `json:"structures" yaml:"structures"`
	Camera     Camera      `json:"camera,omitempty" yaml:"camera,omitempty"`
}

type Canvas struct {
	Background string `json:"background,omitempty" yaml:"background,omitempty"`
}

type Structure struct {
	Ref        string      `json:"ref,omitempty" yaml:"ref,omitempty"`
	Source     string      `json:"source" yaml:"source"`
	Type       string      `json:"type,omitempty" yaml:"type,omitempty"`
	Assembly   string      `json:"assembly,omitempty" yaml:"assembly,omitempty"`
	Components []Component `json:"components,omitempty" yaml:"components,omitempty"`
}

type Component struct {
	Ref            string         `json:"ref,omitempty" yaml:"ref,omitempty"`
	Select         string         `json:"select" yaml:"select"`
	Representation Representation `json:"representation,omitempty" yaml:"representation,omitempty"`
	Label          string         `json:"label,omitempty" yaml:"label,omitempty"`
	Tooltip        string         `json:"tooltip,omitempty" yaml:"tooltip,omitempty"`
}

type Representation struct {
	Type            string  `json:"type,omitempty" yaml:"type,omitempty"`
	Color           string  `json:"color,omitempty" yaml:"color,omitempty"`
	SizeFactor      float64 `json:"size_factor,omitempty" yaml:"size_factor,omitempty"`
	IgnoreHydrogens *bool   `json:"ignore_hydrogens,omitempty" yaml:"ignore_hydrogens,omitempty"`
}

type Camera struct {
	Focus     string    `json:"focus,omitempty" yaml:"focus,omitempty"`
	View      string    `json:"view,omitempty" yaml:"view,omitempty"`
	Zoom      float64   `json:"zoom,omitempty" yaml:"zoom,omitempty"`
	Target    []float64 `json:"target,omitempty" yaml:"target,omitempty"`
	Position  []float64 `json:"position,omitempty" yaml:"position,omitempty"`
	Up        []float64 `json:"up,omitempty" yaml:"up,omitempty"`
	Direction []float64 `json:"direction,omitempty" yaml:"direction,omitempty"`
	Near      *float64  `json:"near,omitempty" yaml:"near,omitempty"`
}

type Output struct {
	Type        string `json:"type" yaml:"type"`
	Path        string `json:"path" yaml:"path"`
	Size        []int  `json:"size,omitempty" yaml:"size,omitempty"`
	Transparent bool   `json:"transparent,omitempty" yaml:"transparent,omitempty"`
	Quality     string `json:"quality,omitempty" yaml:"quality,omitempty"`
}

type ArchiveAsset struct {
	Name string `json:"name" yaml:"name"`
	Path string `json:"path" yaml:"path"`
}

func (j Job) ValidateScene() error {
	if j.Version == 0 {
		return errors.New("version is required")
	}
	if j.Version != 1 {
		return fmt.Errorf("unsupported job version %d", j.Version)
	}
	if len(j.Inputs) == 0 {
		return errors.New("at least one input is required")
	}
	for ref, input := range j.Inputs {
		if strings.TrimSpace(ref) == "" {
			return errors.New("input refs cannot be empty")
		}
		if err := input.Validate(); err != nil {
			return fmt.Errorf("input %q: %w", ref, err)
		}
	}
	if len(j.Scene.Structures) == 0 {
		return errors.New("scene.structures must include at least one structure")
	}
	for i, structure := range j.Scene.Structures {
		if strings.TrimSpace(structure.Source) == "" {
			return fmt.Errorf("scene.structures[%d].source is required", i)
		}
		if _, ok := j.Inputs[structure.Source]; !ok {
			return fmt.Errorf("scene.structures[%d].source references unknown input %q", i, structure.Source)
		}
		for c, component := range structure.Components {
			if strings.TrimSpace(component.Select) == "" {
				return fmt.Errorf("scene.structures[%d].components[%d].select is required", i, c)
			}
		}
	}
	if len(j.Scene.Camera.Target) > 0 && len(j.Scene.Camera.Target) != 3 {
		return errors.New("scene.camera.target must contain exactly three numbers")
	}
	if len(j.Scene.Camera.Position) > 0 && len(j.Scene.Camera.Position) != 3 {
		return errors.New("scene.camera.position must contain exactly three numbers")
	}
	if len(j.Scene.Camera.Up) > 0 && len(j.Scene.Camera.Up) != 3 {
		return errors.New("scene.camera.up must contain exactly three numbers")
	}
	if len(j.Scene.Camera.Direction) > 0 && len(j.Scene.Camera.Direction) != 3 {
		return errors.New("scene.camera.direction must contain exactly three numbers")
	}
	return nil
}

func (j Job) ValidateRender() error {
	if err := j.ValidateScene(); err != nil {
		return err
	}
	if err := j.ValidateRuntimeLimits(); err != nil {
		return err
	}
	if len(j.Outputs) == 0 {
		return errors.New("at least one output is required for rendering")
	}
	for i, output := range j.Outputs {
		if strings.TrimSpace(output.Type) == "" {
			return fmt.Errorf("outputs[%d].type is required", i)
		}
		if strings.TrimSpace(output.Path) == "" {
			return fmt.Errorf("outputs[%d].path is required", i)
		}
		if len(output.Size) > 0 && len(output.Size) != 2 {
			return fmt.Errorf("outputs[%d].size must contain width and height", i)
		}
		// Reject non-positive dimensions here (always), not only under the
		// optional max_pixels check, so a negative/zero size returns a clean
		// validation error instead of crashing the renderer with a 5xx.
		if len(output.Size) == 2 && (output.Size[0] <= 0 || output.Size[1] <= 0) {
			return fmt.Errorf("outputs[%d].size must be positive, got %dx%d", i, output.Size[0], output.Size[1])
		}
	}
	for i, asset := range j.Assets {
		if strings.TrimSpace(asset.Name) == "" {
			return fmt.Errorf("assets[%d].name is required", i)
		}
		if strings.TrimSpace(asset.Path) == "" {
			return fmt.Errorf("assets[%d].path is required", i)
		}
	}
	return nil
}

func (j Job) ValidateRuntimeLimits() error {
	if err := ValidateRuntimeProfile(j.Runtime); err != nil {
		return err
	}
	j.Runtime = ApplyRuntimeProfile(j.Runtime)
	if j.Runtime.MaxOutputs > 0 && len(j.Outputs) > j.Runtime.MaxOutputs {
		return fmt.Errorf("runtime.max_outputs=%d exceeded by %d outputs", j.Runtime.MaxOutputs, len(j.Outputs))
	}
	if j.Runtime.MaxPixels > 0 {
		for i, output := range j.Outputs {
			switch output.NormalizedType() {
			case "image", "video":
				width, height := output.SizeOrDefault(800, 800)
				if width < 0 || height < 0 {
					return fmt.Errorf("outputs[%d] has negative size %dx%d", i, width, height)
				}
				// Overflow-safe: comparing width*height directly can wrap int
				// and let a huge size (e.g. [4611686018427387904, 8]) slip past.
				// width > MaxPixels/height is exactly equivalent for height > 0.
				if height > 0 && width > j.Runtime.MaxPixels/height {
					return fmt.Errorf("outputs[%d] has %dx%d pixels, exceeding runtime.max_pixels=%d", i, width, height, j.Runtime.MaxPixels)
				}
			}
		}
	}
	return nil
}

func (input Input) Validate() error {
	var sources int
	if input.ID != "" {
		sources++
	}
	if input.Path != "" {
		sources++
	}
	if input.URL != "" {
		sources++
	}
	if sources != 1 {
		return errors.New("exactly one of id, path, or url is required")
	}
	if input.URL != "" {
		parsed, err := url.Parse(input.URL)
		if err != nil || parsed.Scheme == "" {
			return fmt.Errorf("url must be absolute: %q", input.URL)
		}
	}
	return nil
}

func (input Input) ResolvedURL() (string, error) {
	switch {
	case input.URL != "":
		return input.URL, nil
	case input.Path != "":
		absolute, err := filepath.Abs(input.Path)
		if err != nil {
			return "", err
		}
		return (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}).String(), nil
	case input.ID != "":
		provider := strings.ToLower(strings.TrimSpace(input.Provider))
		if provider == "" {
			provider = "pdbe"
		}
		id := strings.TrimSpace(input.ID)
		switch provider {
		case "pdbe":
			return fmt.Sprintf("https://www.ebi.ac.uk/pdbe/entry-files/%s.bcif", strings.ToLower(id)), nil
		case "rcsb":
			return fmt.Sprintf("https://models.rcsb.org/%s.bcif", strings.ToUpper(id)), nil
		case "alphafold", "afdb":
			if strings.HasPrefix(strings.ToUpper(id), "AF-") {
				return fmt.Sprintf("https://alphafold.ebi.ac.uk/files/%s.cif", id), nil
			}
			return fmt.Sprintf("https://alphafold.ebi.ac.uk/files/AF-%s-F1-model_%s.cif", id, alphafoldModelVersion()), nil
		default:
			return "", fmt.Errorf("unsupported provider %q", input.Provider)
		}
	default:
		return "", errors.New("missing input source")
	}
}

// defaultAlphaFoldModelVersion tracks the AlphaFold DB model file version used
// when resolving a bare UniProt accession to a download URL. AlphaFold bumps
// this periodically (…v4 → v5 → v6), which 404s the old files, so it is
// overridable via MOLSTAR_ALPHAFOLD_MODEL_VERSION without a code change. A full
// "AF-…" identifier bypasses this entirely.
const defaultAlphaFoldModelVersion = "v6"

func alphafoldModelVersion() string {
	if v := strings.TrimSpace(os.Getenv("MOLSTAR_ALPHAFOLD_MODEL_VERSION")); v != "" {
		if !strings.HasPrefix(v, "v") {
			v = "v" + v
		}
		return v
	}
	return defaultAlphaFoldModelVersion
}

func (input Input) ResolvedFormat() string {
	if input.Format != "" {
		return NormalizeFormat(input.Format)
	}
	if input.ID != "" {
		provider := strings.ToLower(input.Provider)
		if provider == "alphafold" || provider == "afdb" {
			return "mmcif"
		}
		return "bcif"
	}
	source := input.Path
	if source == "" {
		source = input.URL
	}
	withoutQuery := strings.Split(source, "?")[0]
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(withoutQuery)), ".")
	switch ext {
	case "cif", "mmcif":
		return "mmcif"
	case "bcif":
		return "bcif"
	case "pdb", "pdbqt", "gro", "xyz", "mol", "sdf", "mol2", "lammpstrj", "xtc", "nctraj", "dcd", "trr", "psf", "prmtop", "top", "map", "dx", "dxbin":
		return ext
	default:
		return "mmcif"
	}
}

func NormalizeFormat(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "cif":
		return "mmcif"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func (output Output) NormalizedType() string {
	kind := strings.ToLower(strings.TrimSpace(output.Type))
	switch kind {
	case "png", "jpg", "jpeg":
		return "image"
	case "movie", "animation", "video", "mp4":
		return "video"
	case "state":
		return "molj"
	default:
		return kind
	}
}

func (output Output) SizeOrDefault(defaultWidth, defaultHeight int) (int, int) {
	if len(output.Size) == 2 && output.Size[0] > 0 && output.Size[1] > 0 {
		return output.Size[0], output.Size[1]
	}
	return defaultWidth, defaultHeight
}

func NetworkEnabled(runtime Runtime) bool {
	if runtime.Offline {
		return false
	}
	if runtime.Network == nil {
		return true
	}
	return *runtime.Network
}

func (input Input) RequiresNetwork() bool {
	if input.ID != "" {
		return true
	}
	if input.URL == "" {
		return false
	}
	parsed, err := url.Parse(input.URL)
	if err != nil {
		return true
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func (input Input) LocalPath() string {
	if input.Path != "" {
		return input.Path
	}
	if input.URL == "" {
		return ""
	}
	parsed, err := url.Parse(input.URL)
	if err != nil || parsed.Scheme != "file" {
		return ""
	}
	return parsed.Path
}

func PathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
