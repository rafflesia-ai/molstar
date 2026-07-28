package mvs

import (
	"strings"
	"testing"

	"github.com/rafflesia-ai/molstar/internal/job"
)

func focusJob(focus string) job.Job {
	return job.Job{
		Version: 1,
		Inputs:  map[string]job.Input{"protein": {ID: "1cbs", Provider: "pdbe"}},
		Scene: job.Scene{
			Structures: []job.Structure{{
				Source: "protein",
				Components: []job.Component{
					{Ref: "polymer", Select: "polymer", Representation: job.Representation{Type: "cartoon"}},
				},
			}},
			Camera: job.Camera{Focus: focus},
		},
	}
}

func TestCompileFocusUnknownTargetListsAvailable(t *testing.T) {
	// A focus that names neither a component nor all/root must fail with a
	// message that lists what IS focusable, not one that implies any selector works.
	_, err := Compile(focusJob("ligand"))
	if err == nil {
		t.Fatal("expected focus on a missing component to error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "polymer") {
		t.Fatalf("focus error should list the available target %q, got: %v", "polymer", err)
	}
	if strings.Contains(msg, "or selector") {
		t.Fatalf("focus error should not imply any selector works: %v", err)
	}

	// Focusing a real component ref, and the special "all"/"root", must succeed.
	for _, focus := range []string{"polymer", "all", "root"} {
		if _, err := Compile(focusJob(focus)); err != nil {
			t.Fatalf("focus %q should compile: %v", focus, err)
		}
	}
}

func TestCompileBasicJob(t *testing.T) {
	j := job.Job{
		Version: 1,
		Inputs: map[string]job.Input{
			"protein": {ID: "1cbs", Provider: "pdbe"},
		},
		Scene: job.Scene{
			Canvas: job.Canvas{Background: "white"},
			Structures: []job.Structure{{
				Source: "protein",
				Components: []job.Component{
					{Ref: "polymer", Select: "polymer", Representation: job.Representation{Type: "cartoon", Color: "chain"}},
					{Ref: "ligand", Select: "ligand", Representation: job.Representation{Type: "ball-and-stick", Color: "#cc3399"}},
				},
			}},
			Camera: job.Camera{Focus: "ligand", Zoom: 1.25},
		},
	}
	result, err := Compile(j)
	if err != nil {
		t.Fatal(err)
	}
	if result.Document.Root.Kind != "root" {
		t.Fatalf("root kind = %q", result.Document.Root.Kind)
	}
	if len(result.Document.Root.Children) != 2 {
		t.Fatalf("expected download and canvas nodes, got %d", len(result.Document.Root.Children))
	}
	download := result.Document.Root.Children[0]
	if download.Kind != "download" {
		t.Fatalf("first node kind = %q", download.Kind)
	}
	if download.Params["url"] != "https://www.ebi.ac.uk/pdbe/entry-files/1cbs.bcif" {
		t.Fatalf("unexpected url: %v", download.Params["url"])
	}
	structure := download.Children[0].Children[0]
	polymer := structure.Children[0]
	if polymer.Ref != "polymer" {
		t.Fatalf("expected polymer ref, got %q", polymer.Ref)
	}
	polymerRepr := polymer.Children[0]
	if polymerRepr.Children[0].Custom["molstar_color_theme_name"] != "chain-id" {
		t.Fatalf("expected chain-id custom theme, got %#v", polymerRepr.Children[0])
	}
	ligand := structure.Children[1]
	if ligand.Children[1].Kind != "focus" {
		t.Fatalf("expected ligand focus node, got %#v", ligand.Children)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %#v", result.Warnings)
	}
}

func TestCompileExplicitColor(t *testing.T) {
	j := job.Job{
		Version: 1,
		Inputs: map[string]job.Input{
			"input": {URL: "https://example.com/model.cif"},
		},
		Scene: job.Scene{
			Structures: []job.Structure{{
				Source: "input",
				Components: []job.Component{{
					Select: "all",
					Representation: job.Representation{
						Type:  "spacefill",
						Color: "red",
					},
				}},
			}},
		},
	}
	result, err := Compile(j)
	if err != nil {
		t.Fatal(err)
	}
	repr := result.Document.Root.Children[0].Children[0].Children[0].Children[0].Children[0]
	if repr.Kind != "representation" {
		t.Fatalf("expected representation, got %q", repr.Kind)
	}
	if len(repr.Children) != 1 || repr.Children[0].Kind != "color" {
		t.Fatalf("expected explicit color child, got %#v", repr.Children)
	}
}

func TestCompileElementColorLowersToMVSSelectors(t *testing.T) {
	j := job.Job{
		Version: 1,
		Inputs: map[string]job.Input{
			"input": {URL: "https://example.com/model.cif"},
		},
		Scene: job.Scene{
			Structures: []job.Structure{{
				Source: "input",
				Components: []job.Component{{
					Select: "ligand",
					Representation: job.Representation{
						Type:  "ball-and-stick",
						Color: "element",
					},
				}},
			}},
		},
	}
	result, err := Compile(j)
	if err != nil {
		t.Fatal(err)
	}
	repr := result.Document.Root.Children[0].Children[0].Children[0].Children[0].Children[0]
	if len(repr.Children) < 4 {
		t.Fatalf("expected selector colors, got %#v", repr.Children)
	}
	if repr.Children[0].Custom != nil {
		t.Fatalf("element color should not depend on custom Mol* theme metadata: %#v", repr.Children[0])
	}
	foundOxygen := false
	for _, child := range repr.Children {
		selector, ok := child.Params["selector"].(map[string]any)
		if !ok {
			continue
		}
		if selector["type_symbol"] == "O" && child.Params["color"] == "#ff0d0d" {
			foundOxygen = true
			break
		}
	}
	if !foundOxygen {
		t.Fatalf("expected oxygen selector color, got %#v", repr.Children)
	}
}

func TestCompileSelectorDSL(t *testing.T) {
	j := job.Job{
		Version: 1,
		Inputs: map[string]job.Input{
			"input": {URL: "https://example.com/model.cif"},
		},
		Scene: job.Scene{Structures: []job.Structure{{
			Source: "input",
			Components: []job.Component{{
				Ref:    "chain_range",
				Select: "chain:A/residue:10-12",
				Representation: job.Representation{
					Type: "cartoon",
				},
			}, {
				Ref:    "retinol",
				Select: "ligand:RTL",
				Representation: job.Representation{
					Type:  "ball-and-stick",
					Color: "element",
				},
			}, {
				Ref:    "near_retinol",
				Select: "within:5A:ligand:RTL",
				Representation: job.Representation{
					Type: "ball-and-stick",
				},
			}},
		}}},
	}
	result, err := Compile(j)
	if err != nil {
		t.Fatal(err)
	}
	structure := result.Document.Root.Children[0].Children[0].Children[0]
	chain := structure.Children[0]
	selector, ok := chain.Params["selector"].(map[string]any)
	if !ok {
		t.Fatalf("expected expression selector, got %#v", chain.Params["selector"])
	}
	if selector["label_asym_id"] != "A" || selector["beg_label_seq_id"] != 10 || selector["end_label_seq_id"] != 12 {
		t.Fatalf("unexpected chain range selector: %#v", selector)
	}
	ligand := structure.Children[1]
	ligandSelector, ok := ligand.Params["selector"].(map[string]any)
	if !ok || ligandSelector["label_comp_id"] != "RTL" {
		t.Fatalf("unexpected ligand selector: %#v", ligand.Params["selector"])
	}
	near := structure.Children[2]
	if near.Custom["molstar_show_non_covalent_interactions"] != true {
		t.Fatalf("expected surroundings custom props, got %#v", near.Custom)
	}
	if len(result.Warnings) == 0 {
		t.Fatalf("expected within selector warning")
	}
}

func TestCompileRejectsUnsupportedSelectorNegation(t *testing.T) {
	j := job.Job{
		Version: 1,
		Inputs: map[string]job.Input{
			"input": {URL: "https://example.com/model.cif"},
		},
		Scene: job.Scene{Structures: []job.Structure{{
			Source: "input",
			Components: []job.Component{{
				Select: "not:water",
			}},
		}}},
	}
	if _, err := Compile(j); err == nil {
		t.Fatal("expected unsupported negation selector error")
	}
}

// The DSL has no boolean operators, but "chain:A and residue:5" used to parse
// as label_asym_id "A and residue": a selector that matches nothing, reported as
// valid by `selectors explain`.
func TestCompileRejectsWhitespaceInSelectorValues(t *testing.T) {
	for _, selector := range []string{
		"chain:A and residue:5",
		"chain:A B C",
		"ligand:RET or HEM",
		"chain:A/residue:5 and atom:CA",
	} {
		if _, _, _, err := compileSelector(selector); err == nil {
			t.Fatalf("selector %q should be rejected, not silently compiled", selector)
		}
	}

	// Surrounding whitespace is still trimmed, and valid selectors keep working.
	for _, selector := range []string{
		" chain:A ",
		"chain:A/residue:10-20",
		"ligand:RET",
		"within:5A:ligand:RTL",
		"polymer",
	} {
		if _, _, _, err := compileSelector(selector); err != nil {
			t.Fatalf("selector %q should compile, got %v", selector, err)
		}
	}
}

// pLDDT must use Mol*'s AlphaFold palette (orange = very low confidence, dark
// blue = very high). It previously mapped to `uncertainty`, which colors high
// values red and so inverted every AlphaFold confidence render: folded domains
// came out red and disordered loops blue.
func TestPLDDTUsesTheAlphaFoldConfidenceTheme(t *testing.T) {
	for _, requested := range []string{"plddt", "pLDDT", "confidence", "model-confidence", "model_confidence"} {
		theme, ok := molstarColorTheme(requested)
		if !ok {
			t.Fatalf("color %q should map to a Mol* theme", requested)
		}
		if theme != "plddt-confidence" {
			t.Fatalf("color %q mapped to %q, want plddt-confidence", requested, theme)
		}
	}

	// `uncertainty` stays available as its own distinct request.
	if theme, ok := molstarColorTheme("uncertainty"); !ok || theme != "uncertainty" {
		t.Fatalf("uncertainty mapped to %q (ok=%v), want uncertainty", theme, ok)
	}
}

func TestIsDocumentBytes(t *testing.T) {
	data := []byte(`{"metadata":{"version":"1"},"root":{"kind":"root"}}`)
	if !IsDocumentBytes(data) {
		t.Fatal("expected MVS bytes to be detected")
	}
	if IsDocumentBytes([]byte(`{"version":1}`)) {
		t.Fatal("job bytes should not be detected as MVS")
	}
}
