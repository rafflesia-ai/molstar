package recipe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sacha-ichbiah/molstar/internal/job"
	"gopkg.in/yaml.v3"
)

type Recipe struct {
	Version       int             `json:"version" yaml:"version"`
	Kind          string          `json:"kind,omitempty" yaml:"kind,omitempty"`
	Name          string          `json:"name,omitempty" yaml:"name,omitempty"`
	Preset        string          `json:"preset,omitempty" yaml:"preset,omitempty"`
	Input         job.Input       `json:"input,omitempty" yaml:"input,omitempty"`
	ID            string          `json:"id,omitempty" yaml:"id,omitempty"`
	Provider      string          `json:"provider,omitempty" yaml:"provider,omitempty"`
	Path          string          `json:"path,omitempty" yaml:"path,omitempty"`
	URL           string          `json:"url,omitempty" yaml:"url,omitempty"`
	Format        string          `json:"format,omitempty" yaml:"format,omitempty"`
	Assembly      string          `json:"assembly,omitempty" yaml:"assembly,omitempty"`
	Runtime       job.Runtime     `json:"runtime,omitempty" yaml:"runtime,omitempty"`
	Background    string          `json:"background,omitempty" yaml:"background,omitempty"`
	StructureType string          `json:"structure_type,omitempty" yaml:"structure_type,omitempty"`
	Focus         string          `json:"focus,omitempty" yaml:"focus,omitempty"`
	View          string          `json:"view,omitempty" yaml:"view,omitempty"`
	Zoom          float64         `json:"zoom,omitempty" yaml:"zoom,omitempty"`
	Size          []int           `json:"size,omitempty" yaml:"size,omitempty"`
	Components    []job.Component `json:"components,omitempty" yaml:"components,omitempty"`
	Outputs       []job.Output    `json:"outputs,omitempty" yaml:"outputs,omitempty"`
}

func LoadBytes(data []byte, name string) (Recipe, error) {
	r, err := Decode(data, name)
	if err != nil {
		return Recipe{}, err
	}
	return r, r.Validate()
}

func Decode(data []byte, name string) (Recipe, error) {
	var r Recipe
	if strings.HasSuffix(strings.ToLower(name), ".json") || json.Valid(data) {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&r); err != nil {
			return Recipe{}, fmt.Errorf("decode recipe json: %w", err)
		}
		return r, nil
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&r); err != nil {
		return Recipe{}, fmt.Errorf("decode recipe yaml: %w", err)
	}
	return r, nil
}

func (r Recipe) Validate() error {
	if r.Version == 0 {
		return fmt.Errorf("recipe version is required")
	}
	if r.Version != 1 {
		return fmt.Errorf("unsupported recipe version %d", r.Version)
	}
	if r.Kind != "" && r.Kind != "recipe" && r.Kind != "molstar-recipe" {
		return fmt.Errorf("unsupported recipe kind %q", r.Kind)
	}
	input := r.NormalizedInput()
	if err := input.Validate(); err != nil {
		return fmt.Errorf("input: %w", err)
	}
	if len(r.Size) > 0 && len(r.Size) != 2 {
		return fmt.Errorf("size must contain width and height")
	}
	for i, output := range r.Outputs {
		if strings.TrimSpace(output.Type) == "" {
			return fmt.Errorf("outputs[%d].type is required", i)
		}
		if strings.TrimSpace(output.Path) == "" {
			return fmt.Errorf("outputs[%d].path is required", i)
		}
	}
	return nil
}

func (r Recipe) NormalizedInput() job.Input {
	input := r.Input
	if input.ID == "" && input.Path == "" && input.URL == "" {
		input.ID = r.ID
		input.Path = r.Path
		input.URL = r.URL
	}
	if input.Provider == "" {
		input.Provider = r.Provider
	}
	if input.Format == "" {
		input.Format = r.Format
	}
	if input.Assembly == "" {
		input.Assembly = r.Assembly
	}
	if input.Provider == "" && input.ID != "" {
		input.Provider = "pdbe"
	}
	return input
}

func (r Recipe) LooksLikeRecipe() bool {
	return r.Kind == "recipe" ||
		r.Kind == "molstar-recipe" ||
		r.Preset != "" ||
		r.Input.ID != "" ||
		r.Input.Path != "" ||
		r.Input.URL != "" ||
		r.ID != "" ||
		r.Path != "" ||
		r.URL != ""
}
