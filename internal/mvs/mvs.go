package mvs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rafflesia-ai/molstar/internal/job"
)

type Document struct {
	Metadata Metadata `json:"metadata"`
	Root     Node     `json:"root"`
}

type Metadata struct {
	Title     string `json:"title,omitempty"`
	Version   string `json:"version"`
	Timestamp string `json:"timestamp,omitempty"`
}

type Node struct {
	Kind     string         `json:"kind"`
	Params   map[string]any `json:"params,omitempty"`
	Custom   map[string]any `json:"custom,omitempty"`
	Ref      string         `json:"ref,omitempty"`
	Children []Node         `json:"children,omitempty"`
}

type CompileResult struct {
	Document        Document
	Warnings        []string
	ThemeExtensions []ThemeExtension
}

type ThemeExtension struct {
	Component string `json:"component" yaml:"component"`
	Requested string `json:"requested" yaml:"requested"`
	Kind      string `json:"kind" yaml:"kind"`
	Theme     string `json:"theme,omitempty" yaml:"theme,omitempty"`
	Mechanism string `json:"mechanism" yaml:"mechanism"`
}

type CompileOption func(*compileConfig)

type compileConfig struct {
	title     string
	timestamp string
}

func WithTitle(title string) CompileOption {
	return func(config *compileConfig) {
		config.title = title
	}
}

func WithTimestamp(t time.Time) CompileOption {
	return func(config *compileConfig) {
		config.timestamp = t.UTC().Format(time.RFC3339Nano)
	}
}

func Compile(j job.Job, options ...CompileOption) (CompileResult, error) {
	if err := j.ValidateScene(); err != nil {
		return CompileResult{}, err
	}
	config := compileConfig{title: "headless molstar scene"}
	for _, option := range options {
		option(&config)
	}
	result := CompileResult{
		Document: Document{
			Metadata: Metadata{
				Title:     config.title,
				Version:   "1",
				Timestamp: config.timestamp,
			},
			Root: Node{Kind: "root"},
		},
	}

	focusTarget := j.Scene.Camera.Focus
	hasExplicitCamera := len(j.Scene.Camera.Target) == 3 && len(j.Scene.Camera.Position) == 3
	focusApplied := false

	for _, structure := range j.Scene.Structures {
		input := j.Inputs[structure.Source]
		url, err := input.ResolvedURL()
		if err != nil {
			return CompileResult{}, err
		}

		structureNode := Node{
			Kind: "structure",
			Ref:  strings.TrimSpace(structure.Ref),
			Params: map[string]any{
				"type": normalizedStructureType(structure, input),
			},
		}
		assemblyID := firstNonEmpty(structure.Assembly, input.Assembly)
		if assemblyID != "" {
			structureNode.Params["assembly_id"] = assemblyID
		}
		for _, component := range structure.Components {
			componentNode, warnings, themeExtensions, err := compileComponent(component)
			if err != nil {
				return CompileResult{}, err
			}
			result.Warnings = append(result.Warnings, warnings...)
			result.ThemeExtensions = append(result.ThemeExtensions, themeExtensions...)
			if focusTarget != "" && !hasExplicitCamera && matchesFocus(component, focusTarget) {
				componentNode.Children = append(componentNode.Children, focusNode(j.Scene.Camera))
				focusApplied = true
			}
			structureNode.Children = append(structureNode.Children, componentNode)
		}

		parseNode := Node{
			Kind: "parse",
			Params: map[string]any{
				"format": input.ResolvedFormat(),
			},
			Children: []Node{structureNode},
		}
		downloadNode := Node{
			Kind: "download",
			Params: map[string]any{
				"url": url,
			},
			Children: []Node{parseNode},
		}
		result.Document.Root.Children = append(result.Document.Root.Children, downloadNode)
	}

	if background := strings.TrimSpace(j.Scene.Canvas.Background); background != "" {
		result.Document.Root.Children = append(result.Document.Root.Children, Node{
			Kind: "canvas",
			Params: map[string]any{
				"background_color": background,
			},
		})
	}

	if hasExplicitCamera {
		result.Document.Root.Children = append(result.Document.Root.Children, cameraNode(j.Scene.Camera))
		if focusTarget != "" {
			result.Warnings = append(result.Warnings, "scene.camera.focus was ignored because explicit target/position camera was provided")
		}
	} else if focusTarget != "" {
		if strings.EqualFold(focusTarget, "all") || strings.EqualFold(focusTarget, "root") {
			result.Document.Root.Children = append(result.Document.Root.Children, focusNode(j.Scene.Camera))
			focusApplied = true
		}
		if !focusApplied {
			targets := availableFocusTargets(j)
			if len(targets) == 0 {
				return CompileResult{}, fmt.Errorf("scene.camera.focus %q must name a component in the scene or \"all\"/\"root\"", focusTarget)
			}
			return CompileResult{}, fmt.Errorf("scene.camera.focus %q must name a component ref or select in the scene (one of: %s) or \"all\"/\"root\"", focusTarget, strings.Join(targets, ", "))
		}
	} else if len(j.Scene.Camera.Direction) > 0 || j.Scene.Camera.View != "" || j.Scene.Camera.Zoom > 0 {
		result.Warnings = append(result.Warnings, "camera direction, view, and zoom require scene.camera.focus unless target/position is set")
	}

	return result, nil
}

func WriteFile(path string, document Document) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := Marshal(document)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func Marshal(document Document) ([]byte, error) {
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func Decode(data []byte) (Document, error) {
	var document Document
	if err := json.Unmarshal(data, &document); err != nil {
		return Document{}, err
	}
	if document.Root.Kind != "root" {
		return Document{}, fmt.Errorf("MVS document root.kind must be root")
	}
	if document.Metadata.Version == "" {
		return Document{}, fmt.Errorf("MVS document metadata.version is required")
	}
	return document, nil
}

func IsDocumentBytes(data []byte) bool {
	_, err := Decode(data)
	return err == nil
}

func compileComponent(component job.Component) (Node, []string, []ThemeExtension, error) {
	selector, selectorWarnings, custom, err := compileSelector(component.Select)
	if err != nil {
		return Node{}, nil, nil, err
	}
	node := Node{
		Kind: "component",
		Ref:  strings.TrimSpace(component.Ref),
		Params: map[string]any{
			"selector": selector,
		},
	}
	if custom != nil {
		node.Custom = custom
	}
	var warnings []string
	warnings = append(warnings, selectorWarnings...)
	var themeExtensions []ThemeExtension
	reprType := normalizeRepresentation(component.Representation.Type)
	repr := Node{
		Kind: "representation",
		Params: map[string]any{
			"type": reprType,
		},
	}
	if component.Representation.SizeFactor > 0 {
		repr.Params["size_factor"] = component.Representation.SizeFactor
	}
	if component.Representation.IgnoreHydrogens != nil {
		repr.Params["ignore_hydrogens"] = *component.Representation.IgnoreHydrogens
	}
	color := strings.TrimSpace(component.Representation.Color)
	if color != "" && color != "default" {
		if colors, ok := staticThemeColors(color); ok {
			repr.Children = append(repr.Children, colors...)
			themeExtensions = append(themeExtensions, ThemeExtension{
				Component: componentName(component),
				Requested: color,
				Kind:      "static",
				Mechanism: "explicit MVS color nodes",
			})
		} else if theme, ok := molstarColorTheme(color); ok {
			repr.Children = append(repr.Children, Node{
				Kind:   "color",
				Params: map[string]any{},
				Custom: map[string]any{
					"molstar_color_theme_name": theme,
				},
			})
			themeExtensions = append(themeExtensions, ThemeExtension{
				Component: componentName(component),
				Requested: color,
				Kind:      "molstar-custom-theme",
				Theme:     theme,
				Mechanism: "custom.molstar_color_theme_name",
			})
		} else {
			repr.Children = append(repr.Children, Node{
				Kind: "color",
				Params: map[string]any{
					"color": color,
				},
			})
		}
	}
	node.Children = append(node.Children, repr)
	if component.Label != "" {
		node.Children = append(node.Children, Node{
			Kind: "label",
			Params: map[string]any{
				"text": component.Label,
			},
		})
	}
	if component.Tooltip != "" {
		node.Children = append(node.Children, Node{
			Kind: "tooltip",
			Params: map[string]any{
				"text": component.Tooltip,
			},
		})
	}
	return node, warnings, themeExtensions, nil
}

func cameraNode(camera job.Camera) Node {
	params := map[string]any{
		"target":   camera.Target,
		"position": camera.Position,
	}
	if len(camera.Up) == 3 {
		params["up"] = camera.Up
	}
	if camera.Near != nil {
		params["near"] = *camera.Near
	}
	return Node{Kind: "camera", Params: params}
}

func focusNode(camera job.Camera) Node {
	params := map[string]any{}
	direction, up, ok := viewVectors(camera.View)
	if ok {
		params["direction"] = direction
		params["up"] = up
	}
	if len(camera.Direction) == 3 {
		params["direction"] = camera.Direction
	}
	if len(camera.Up) == 3 {
		params["up"] = camera.Up
	}
	if camera.Zoom > 0 {
		params["radius_factor"] = 1 / camera.Zoom
	}
	return Node{Kind: "focus", Params: params}
}

func normalizedStructureType(structure job.Structure, input job.Input) string {
	if structure.Type != "" {
		return strings.ToLower(strings.TrimSpace(structure.Type))
	}
	if structure.Assembly != "" || input.Assembly != "" {
		return "assembly"
	}
	return "model"
}

func normalizeRepresentation(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "cartoon":
		return "cartoon"
	case "ball-and-stick", "ball_and_stick", "ballstick":
		return "ball_and_stick"
	case "space-fill", "space_fill", "spacefill":
		return "spacefill"
	default:
		return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "-", "_")
	}
}

func staticThemeColors(value string) ([]Node, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "element", "element-symbol", "element_symbol":
		colors := []Node{{
			Kind: "color",
			Params: map[string]any{
				"color": "#909090",
			},
		}}
		for _, entry := range []struct {
			element string
			color   string
		}{
			{"H", "#ffffff"},
			{"C", "#909090"},
			{"N", "#3050f8"},
			{"O", "#ff0d0d"},
			{"S", "#ffff30"},
			{"P", "#ff8000"},
			{"FE", "#e06633"},
			{"MG", "#8aff00"},
			{"ZN", "#7d80b0"},
			{"CL", "#1ff01f"},
		} {
			colors = append(colors, Node{
				Kind: "color",
				Params: map[string]any{
					"selector": map[string]any{
						"type_symbol": entry.element,
					},
					"color": entry.color,
				},
			})
		}
		return colors, true
	default:
		return nil, false
	}
}

func molstarColorTheme(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "chain", "chain-id", "chain_id":
		return "chain-id", true
	case "entity", "entity-id", "entity_id":
		return "entity-id", true
	case "element", "element-symbol", "element_symbol":
		return "element-symbol", true
	case "plddt", "confidence", "model-confidence", "model_confidence", "uncertainty":
		return "uncertainty", true
	default:
		return "", false
	}
}

func matchesFocus(component job.Component, focus string) bool {
	return strings.EqualFold(component.Ref, focus) || strings.EqualFold(component.Select, focus)
}

// availableFocusTargets lists the distinct component refs and select expressions
// a scene.camera.focus value can point at, so a focus error can tell the user
// what is actually focusable instead of implying any selector works.
func availableFocusTargets(j job.Job) []string {
	var targets []string
	seen := map[string]bool{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		targets = append(targets, value)
	}
	for _, structure := range j.Scene.Structures {
		for _, component := range structure.Components {
			add(component.Ref)
			add(component.Select)
		}
	}
	return targets
}

func componentName(component job.Component) string {
	if component.Ref != "" {
		return component.Ref
	}
	return component.Select
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func viewVectors(view string) ([]float64, []float64, bool) {
	switch strings.ToLower(strings.TrimSpace(view)) {
	case "":
		return nil, nil, false
	case "front":
		return []float64{0, 0, -1}, []float64{0, 1, 0}, true
	case "back":
		return []float64{0, 0, 1}, []float64{0, 1, 0}, true
	case "top":
		return []float64{0, -1, 0}, []float64{0, 0, -1}, true
	case "bottom":
		return []float64{0, 1, 0}, []float64{0, 0, 1}, true
	case "left":
		return []float64{-1, 0, 0}, []float64{0, 1, 0}, true
	case "right":
		return []float64{1, 0, 0}, []float64{0, 1, 0}, true
	default:
		return nil, nil, false
	}
}
