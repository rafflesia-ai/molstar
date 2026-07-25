package mvs

import (
	"fmt"
	"strconv"
	"strings"
)

var staticSelectors = map[string]string{
	"all":      "all",
	"polymer":  "polymer",
	"protein":  "protein",
	"nucleic":  "nucleic",
	"branched": "branched",
	"ligand":   "ligand",
	"ion":      "ion",
	"water":    "water",
	"coarse":   "coarse",
}

type SelectorExample struct {
	Selector    string `json:"selector" yaml:"selector"`
	Description string `json:"description" yaml:"description"`
}

type SelectorExplanation struct {
	Input    string         `json:"input" yaml:"input"`
	Selector any            `json:"selector" yaml:"selector"`
	Custom   map[string]any `json:"custom,omitempty" yaml:"custom,omitempty"`
	Warnings []string       `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

func SelectorExamples() []SelectorExample {
	return []SelectorExample{
		{Selector: "all", Description: "All atoms in the structure."},
		{Selector: "polymer", Description: "Polymer chains."},
		{Selector: "protein", Description: "Protein polymer chains."},
		{Selector: "nucleic", Description: "Nucleic acid polymer chains."},
		{Selector: "ligand", Description: "Non-polymer ligand components."},
		{Selector: "ion", Description: "Ion components."},
		{Selector: "water", Description: "Water components."},
		{Selector: "chain:A", Description: "Label asym chain A."},
		{Selector: "auth-chain:A", Description: "Author asym chain A."},
		{Selector: "chain:A/residue:10-20", Description: "Residues 10 through 20 on label asym chain A."},
		{Selector: "ligand:RET", Description: "Residue/component name RET."},
		{Selector: "atom:CA", Description: "Atoms with label atom id CA."},
		{Selector: "atom:123", Description: "Atom with source atom id 123."},
		{Selector: "element:C", Description: "Carbon atoms."},
		{Selector: "within:5A:ligand", Description: "Mol* surroundings extension around ligands within 5 Angstrom."},
	}
}

func ExplainSelector(value string) (SelectorExplanation, error) {
	selector, warnings, custom, err := compileSelector(value)
	if err != nil {
		return SelectorExplanation{}, err
	}
	return SelectorExplanation{
		Input:    strings.TrimSpace(value),
		Selector: selector,
		Custom:   custom,
		Warnings: warnings,
	}, nil
}

func compileSelector(value string) (any, []string, map[string]any, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return nil, nil, nil, fmt.Errorf("selector is empty")
	}
	normalized := strings.ToLower(raw)
	if selector, ok := staticSelectors[normalized]; ok {
		return selector, nil, nil, nil
	}
	if strings.HasPrefix(normalized, "not:") {
		return nil, nil, nil, fmt.Errorf("selector %q is not supported: portable MVS selectors do not support negation; use explicit included selectors such as polymer, ligand, ion, or chain:A", raw)
	}
	if strings.HasPrefix(normalized, "within:") {
		return compileWithinSelector(raw)
	}
	if strings.Contains(raw, ",") {
		parts := strings.Split(raw, ",")
		var expressions []any
		var warnings []string
		for _, part := range parts {
			selector, nestedWarnings, custom, err := compileSelector(part)
			if err != nil {
				return nil, nil, nil, err
			}
			if custom != nil {
				return nil, nil, nil, fmt.Errorf("selector %q cannot combine within: selectors with comma unions", raw)
			}
			expression, ok := selector.(map[string]any)
			if !ok {
				return nil, nil, nil, fmt.Errorf("selector %q cannot mix static selectors with expression unions", raw)
			}
			expressions = append(expressions, expression)
			warnings = append(warnings, nestedWarnings...)
		}
		return expressions, warnings, nil, nil
	}
	return compileExpressionSelector(raw)
}

func compileWithinSelector(raw string) (any, []string, map[string]any, error) {
	rest := strings.TrimSpace(raw[len("within:"):])
	first := strings.Index(rest, ":")
	if first < 0 {
		return nil, nil, nil, fmt.Errorf("selector %q must use within:5A:target", raw)
	}
	radiusText := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(rest[:first]), "A"), "a")
	radius, err := strconv.ParseFloat(radiusText, 64)
	if err != nil || radius <= 0 {
		return nil, nil, nil, fmt.Errorf("selector %q has invalid radius %q", raw, rest[:first])
	}
	target := strings.TrimSpace(rest[first+1:])
	selector, warnings, nestedCustom, err := compileSelector(target)
	if err != nil {
		return nil, nil, nil, err
	}
	if nestedCustom != nil {
		return nil, nil, nil, fmt.Errorf("selector %q cannot nest within: selectors", raw)
	}
	custom := map[string]any{
		"molstar_show_non_covalent_interactions":       true,
		"molstar_non_covalent_interactions_radius_ang": radius,
	}
	warnings = append(warnings, fmt.Sprintf("selector %q uses Mol* surroundings extension around %q at %.2f A", raw, target, radius))
	return selector, warnings, custom, nil
}

func compileExpressionSelector(raw string) (map[string]any, []string, map[string]any, error) {
	expression := map[string]any{}
	for _, segment := range strings.Split(raw, "/") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		if err := applySelectorSegment(expression, segment); err != nil {
			return nil, nil, nil, err
		}
	}
	if len(expression) == 0 {
		return nil, nil, nil, fmt.Errorf("selector %q is empty", raw)
	}
	return expression, nil, nil, nil
}

func applySelectorSegment(expression map[string]any, segment string) error {
	parts := strings.Split(segment, ":")
	if len(parts) < 2 {
		return fmt.Errorf("selector segment %q must use key:value syntax", segment)
	}
	key := strings.ToLower(strings.TrimSpace(parts[0]))
	value := strings.TrimSpace(parts[1])
	if value == "" {
		return fmt.Errorf("selector segment %q has an empty value", segment)
	}
	switch key {
	case "chain", "label-chain", "label_chain", "label_asym_id":
		expression["label_asym_id"] = value
		if len(parts) == 3 {
			return applyResidueRange(expression, strings.TrimSpace(parts[2]), false)
		}
	case "auth-chain", "auth_chain", "auth_asym_id":
		expression["auth_asym_id"] = value
		if len(parts) == 3 {
			return applyResidueRange(expression, strings.TrimSpace(parts[2]), true)
		}
	case "entity", "label_entity_id":
		expression["label_entity_id"] = value
	case "residue", "res", "label_seq_id":
		return applyResidueRange(expression, value, false)
	case "auth-residue", "auth_residue", "auth_seq_id":
		return applyResidueRange(expression, value, true)
	case "ligand", "resname", "comp", "component", "label_comp_id":
		expression["label_comp_id"] = strings.ToUpper(value)
	case "auth-comp", "auth_comp", "auth_comp_id":
		expression["auth_comp_id"] = strings.ToUpper(value)
	case "atom", "label_atom_id":
		if id, ok := parseInt(value); ok {
			expression["atom_id"] = id
		} else {
			expression["label_atom_id"] = value
		}
	case "auth-atom", "auth_atom", "auth_atom_id":
		expression["auth_atom_id"] = value
	case "atom-index", "atom_index":
		id, ok := parseInt(value)
		if !ok {
			return fmt.Errorf("selector segment %q requires an integer atom index", segment)
		}
		expression["atom_index"] = id
	case "element", "type_symbol":
		expression["type_symbol"] = strings.ToUpper(value)
	case "instance", "instance_id":
		expression["instance_id"] = value
	default:
		return fmt.Errorf("unsupported selector segment %q; supported keys include chain, auth-chain, residue, ligand, atom, element, and instance", key)
	}
	if len(parts) > 3 {
		return fmt.Errorf("selector segment %q has too many ':' parts", segment)
	}
	return nil
}

func applyResidueRange(expression map[string]any, value string, auth bool) error {
	if strings.Contains(value, "-") {
		parts := strings.SplitN(value, "-", 2)
		start, ok := parseInt(strings.TrimSpace(parts[0]))
		if !ok {
			return fmt.Errorf("invalid residue range start %q", parts[0])
		}
		end, ok := parseInt(strings.TrimSpace(parts[1]))
		if !ok {
			return fmt.Errorf("invalid residue range end %q", parts[1])
		}
		if end < start {
			return fmt.Errorf("invalid residue range %q: end is before start", value)
		}
		if auth {
			expression["beg_auth_seq_id"] = start
			expression["end_auth_seq_id"] = end
		} else {
			expression["beg_label_seq_id"] = start
			expression["end_label_seq_id"] = end
		}
		return nil
	}
	id, ok := parseInt(value)
	if !ok {
		return fmt.Errorf("invalid residue id %q", value)
	}
	if auth {
		expression["auth_seq_id"] = id
	} else {
		expression["label_seq_id"] = id
	}
	return nil
}

func parseInt(value string) (int, bool) {
	id, err := strconv.Atoi(strings.TrimSpace(value))
	return id, err == nil
}
