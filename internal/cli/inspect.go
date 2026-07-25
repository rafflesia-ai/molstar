package cli

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sacha-ichbiah/molstar/internal/job"
	"github.com/sacha-ichbiah/molstar/internal/mvs"
	"github.com/sacha-ichbiah/molstar/internal/render"
)

type inspectFlags struct {
	selector        string
	noPrepare       bool
	semantic        string
	strictSemantic  bool
	rendererCommand string
	jsonReport      bool
	runtime         runtimeFlags
}

type atomRecord struct {
	group string
	chain string
	comp  string
	seq   string
	x     float64
	y     float64
	z     float64
}

type selectionStats struct {
	Supported      bool       `json:"supported"`
	Atoms          int        `json:"atoms,omitempty"`
	Residues       int        `json:"residues,omitempty"`
	Chains         []string   `json:"chains,omitempty"`
	BoundingBox    *boxReport `json:"bounding_box,omitempty"`
	BoundingSphere *sphere    `json:"bounding_sphere,omitempty"`
}

type boxReport struct {
	Min []float64 `json:"min"`
	Max []float64 `json:"max"`
}

type sphere struct {
	Center []float64 `json:"center"`
	Radius float64   `json:"radius"`
}

func (a app) inspectCommand() *cobra.Command {
	flags := &inspectFlags{jsonReport: true, semantic: "auto"}
	cmd := &cobra.Command{
		Use:   "inspect JOB",
		Short: "Inspect a job's inputs, selections, and planned outputs before rendering",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runWithJSONErrors("inspect", flags.jsonReport, func() error {
				if err := exactArgs(args, 1, "inspect"); err != nil {
					return markError(kindInvalidInput, err)
				}
				report, err := a.runInspect(cmd.Context(), args[0], flags, cmd)
				if err != nil {
					return err
				}
				if flags.jsonReport {
					return writeJSON(a.stdout, report)
				}
				return writeYAML(a.stdout, report)
			})
		},
	}
	cmd.Flags().StringVar(&flags.selector, "select", "", "additional selector to inspect")
	cmd.Flags().BoolVar(&flags.noPrepare, "no-prepare", false, "skip runtime preparation and remote cache resolution")
	cmd.Flags().StringVar(&flags.semantic, "semantic", flags.semantic, "semantic inspection mode: auto, true, or false")
	if semanticFlag := cmd.Flags().Lookup("semantic"); semanticFlag != nil {
		semanticFlag.NoOptDefVal = "true"
	}
	cmd.Flags().BoolVar(&flags.strictSemantic, "strict-semantic", false, "fail if Mol* semantic inspection cannot run")
	cmd.Flags().StringVar(&flags.rendererCommand, "renderer-command", "", "renderer command override for semantic inspection")
	cmd.Flags().BoolVar(&flags.jsonReport, "json", true, "write JSON report")
	bindRuntimeFlags(cmd, &flags.runtime)
	return cmd
}

func (a app) runInspect(ctx context.Context, path string, flags *inspectFlags, cmd *cobra.Command) (map[string]any, error) {
	semanticMode, err := normalizeInspectSemanticMode(flags.semantic)
	if err != nil {
		return nil, markError(kindInvalidInput, err)
	}
	data, name, err := a.readInput(path)
	if err != nil {
		return nil, markError(kindInvalidInput, err)
	}
	if isMVSPath(name) || mvs.IsDocumentBytes(data) {
		return nil, markError(kindInvalidInput, fmt.Errorf("inspect expects a headless job or recipe, but %q is an MVS scene; use `molstar scene validate %s --json` to validate it or `molstar render %s` to render it", name, shellArg(path), shellArg(path)))
	}
	j, err := a.loadJobOrRecipeBytes(data, name, true)
	if err != nil {
		return nil, markError(kindInvalidInput, err)
	}
	applyRuntimeFlags(cmd, &j, flags.runtime)
	report := map[string]any{
		"ok":      true,
		"input":   name,
		"runtime": job.ApplyRuntimeProfile(j.Runtime),
		"inputs":  job.ExplainRuntime(j),
		"outputs": plannedOutputs(j),
	}
	if !flags.noPrepare {
		prepared, runtimeReport, err := prepareJob(ctx, j)
		if err != nil {
			return nil, markError(kindRuntime, err)
		}
		j = prepared
		report["cached_inputs"] = runtimeReport.CachedInputs
	}
	inspectJob := jobWithInspectRefs(j, flags.selector)
	compiled, err := mvs.Compile(inspectJob)
	if err != nil {
		return nil, markError(kindInvalidScene, err)
	}
	report["warnings"] = compiled.Warnings
	report["themes"] = compiled.ThemeExtensions
	report["components"] = inspectComponents(j, flags.selector)
	if semanticMode != "false" {
		if semanticMode == "auto" {
			skipReport, shouldRun, err := a.inspectSemanticPreflight(ctx, flags)
			if err != nil {
				report["semantic"] = skipReport
				if flags.strictSemantic {
					return nil, markError(classifyError(err), err)
				}
				return report, nil
			}
			if !shouldRun {
				report["semantic"] = skipReport
				return report, nil
			}
		}
		semantic, command, err := a.runSemanticInspect(ctx, compiled.Document, flags)
		command.Stdout = ""
		if err != nil {
			semantic = map[string]any{
				"ok":        false,
				"mode":      semanticMode,
				"error":     err.Error(),
				"diagnosis": diagnoseError(err),
				"command":   command,
			}
			report["semantic"] = semantic
			if flags.strictSemantic {
				return nil, markError(classifyError(err), err)
			}
		} else {
			semantic["mode"] = semanticMode
			semantic["command"] = command
			report["semantic"] = semantic
		}
	}
	return report, nil
}

func normalizeInspectSemanticMode(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return "auto", nil
	case "true", "1", "yes", "on":
		return "true", nil
	case "false", "0", "no", "off":
		return "false", nil
	default:
		return "", fmt.Errorf("--semantic must be auto, true, or false")
	}
}

func (a app) inspectSemanticPreflight(ctx context.Context, flags *inspectFlags) (map[string]any, bool, error) {
	runner := render.NewMolstar()
	runner.Stdout = nil
	runner.Stderr = nil
	runner.Quiet = true
	if strings.TrimSpace(flags.rendererCommand) != "" {
		runner.RendererCommand = strings.Fields(flags.rendererCommand)
	}
	if !render.SupportsCapabilities(runner.RendererCommand) {
		return nil, true, nil
	}
	capabilities := runner.Capabilities(ctx)
	command := capabilities.Command
	command.Stdout = ""
	if !capabilities.OK {
		err := fmt.Errorf("semantic inspection renderer capabilities failed: %s", capabilities.Error)
		return map[string]any{
			"ok":        false,
			"mode":      "auto",
			"skipped":   true,
			"error":     err.Error(),
			"diagnosis": diagnoseError(err),
			"command":   command,
		}, false, err
	}
	available, detail, present := rendererGLStatus(capabilities.Renderer)
	if present && !available {
		message := "headless WebGL context is unavailable"
		if detail != "" {
			message += ": " + detail
		}
		err := fmt.Errorf("%s", message)
		return map[string]any{
			"ok":        false,
			"mode":      "auto",
			"skipped":   true,
			"error":     message,
			"diagnosis": diagnoseError(err),
			"command":   command,
		}, false, err
	}
	return nil, true, nil
}

func (a app) runSemanticInspect(ctx context.Context, document mvs.Document, flags *inspectFlags) (map[string]any, render.CommandResult, error) {
	data, err := mvs.Marshal(document)
	if err != nil {
		return nil, render.CommandResult{}, err
	}
	scenePath, cleanup, err := writeTempMVS(data)
	if err != nil {
		return nil, render.CommandResult{}, err
	}
	defer cleanup()
	runner := render.NewMolstar()
	runner.Stdout = nil
	runner.Stderr = a.stderr
	runner.Quiet = true
	if strings.TrimSpace(flags.rendererCommand) != "" {
		runner.RendererCommand = strings.Fields(flags.rendererCommand)
	}
	command, semantic, err := runner.InspectMVS(ctx, scenePath)
	return semantic, command, err
}

func jobWithInspectRefs(j job.Job, extraSelector string) job.Job {
	for structureIndex := range j.Scene.Structures {
		structure := &j.Scene.Structures[structureIndex]
		seen := map[string]bool{}
		if strings.TrimSpace(structure.Ref) == "" {
			structure.Ref = uniqueInspectRef(fmt.Sprintf("structure_%d", structureIndex+1), seen)
		} else {
			seen[structure.Ref] = true
		}
		for componentIndex := range structure.Components {
			component := &structure.Components[componentIndex]
			if strings.TrimSpace(component.Ref) == "" {
				base := sanitizeRef(component.Select)
				if base == "" {
					base = fmt.Sprintf("component_%d", componentIndex+1)
				}
				component.Ref = uniqueInspectRef(base, seen)
			} else {
				component.Ref = uniqueInspectRef(component.Ref, seen)
			}
		}
		if strings.TrimSpace(extraSelector) != "" {
			structure.Components = append(structure.Components, job.Component{
				Ref:    uniqueInspectRef("query", seen),
				Select: extraSelector,
				Representation: job.Representation{
					Type: "spacefill",
				},
			})
		}
	}
	return j
}

func uniqueInspectRef(base string, seen map[string]bool) string {
	ref := sanitizeRef(base)
	if ref == "" {
		ref = "component"
	}
	candidate := ref
	for i := 2; seen[candidate]; i++ {
		candidate = fmt.Sprintf("%s_%d", ref, i)
	}
	seen[candidate] = true
	return candidate
}

func inspectComponents(j job.Job, extraSelector string) []map[string]any {
	statsByInput := map[string][]atomRecord{}
	for ref, input := range j.Inputs {
		if input.LocalPath() == "" {
			continue
		}
		records, err := readAtomRecords(input.LocalPath(), input.ResolvedFormat())
		if err == nil {
			statsByInput[ref] = records
		}
	}
	var components []map[string]any
	for _, structure := range j.Scene.Structures {
		records := statsByInput[structure.Source]
		for _, component := range structure.Components {
			components = append(components, inspectComponent(structure, component, records))
		}
		if extraSelector != "" {
			component := job.Component{Ref: "query", Select: extraSelector}
			components = append(components, inspectComponent(structure, component, records))
		}
	}
	return components
}

func inspectComponent(structure job.Structure, component job.Component, records []atomRecord) map[string]any {
	selected, supported := filterAtoms(records, component.Select)
	return map[string]any{
		"structure":      structure.Ref,
		"source":         structure.Source,
		"ref":            component.Ref,
		"select":         component.Select,
		"representation": component.Representation,
		"stats":          summarizeAtoms(selected, supported),
	}
}

func readAtomRecords(path string, format string) ([]atomRecord, error) {
	if format == "bcif" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var records []atomRecord
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		record, ok := parseAtomRecord(scanner.Text())
		if ok {
			records = append(records, record)
		}
	}
	return records, scanner.Err()
}

func parseAtomRecord(line string) (atomRecord, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "ATOM ") && !strings.HasPrefix(trimmed, "HETATM ") {
		return atomRecord{}, false
	}
	if len(line) >= 54 {
		x, xErr := strconv.ParseFloat(strings.TrimSpace(line[30:38]), 64)
		y, yErr := strconv.ParseFloat(strings.TrimSpace(line[38:46]), 64)
		z, zErr := strconv.ParseFloat(strings.TrimSpace(line[46:54]), 64)
		if xErr == nil && yErr == nil && zErr == nil {
			return atomRecord{
				group: strings.Fields(trimmed)[0],
				chain: strings.TrimSpace(sliceString(line, 21, 22)),
				comp:  strings.TrimSpace(sliceString(line, 17, 20)),
				seq:   strings.TrimSpace(sliceString(line, 22, 26)),
				x:     x, y: y, z: z,
			}, true
		}
	}
	fields := strings.Fields(trimmed)
	if len(fields) < 13 {
		return atomRecord{}, false
	}
	x, xErr := strconv.ParseFloat(fields[10], 64)
	y, yErr := strconv.ParseFloat(fields[11], 64)
	z, zErr := strconv.ParseFloat(fields[12], 64)
	if xErr != nil || yErr != nil || zErr != nil {
		return atomRecord{}, false
	}
	return atomRecord{group: fields[0], comp: fields[5], chain: fields[6], seq: fields[8], x: x, y: y, z: z}, true
}

func sliceString(value string, start int, end int) string {
	if len(value) <= start {
		return ""
	}
	if len(value) < end {
		end = len(value)
	}
	return value[start:end]
}

func filterAtoms(records []atomRecord, selector string) ([]atomRecord, bool) {
	selector = strings.TrimSpace(strings.ToLower(selector))
	if selector == "" || selector == "all" {
		return records, true
	}
	if strings.HasPrefix(selector, "chain:") {
		chain := strings.TrimSpace(strings.TrimPrefix(selector, "chain:"))
		return filterAtomRecords(records, func(atom atomRecord) bool { return strings.EqualFold(atom.chain, chain) }), true
	}
	for _, prefix := range []string{"resname:", "comp:", "component:", "ligand:"} {
		if strings.HasPrefix(selector, prefix) {
			comp := strings.TrimSpace(strings.TrimPrefix(selector, prefix))
			return filterAtomRecords(records, func(atom atomRecord) bool { return strings.EqualFold(atom.comp, comp) }), true
		}
	}
	switch selector {
	case "water":
		return filterAtomRecords(records, func(atom atomRecord) bool {
			return strings.EqualFold(atom.comp, "HOH") || strings.EqualFold(atom.comp, "WAT")
		}), true
	case "ligand":
		return filterAtomRecords(records, func(atom atomRecord) bool {
			return strings.EqualFold(atom.group, "HETATM") && !strings.EqualFold(atom.comp, "HOH") && !strings.EqualFold(atom.comp, "WAT")
		}), true
	default:
		return nil, false
	}
}

func filterAtomRecords(records []atomRecord, keep func(atomRecord) bool) []atomRecord {
	var out []atomRecord
	for _, record := range records {
		if keep(record) {
			out = append(out, record)
		}
	}
	return out
}

func summarizeAtoms(records []atomRecord, supported bool) selectionStats {
	if !supported {
		return selectionStats{Supported: false}
	}
	stats := selectionStats{Supported: true, Atoms: len(records)}
	if len(records) == 0 {
		return stats
	}
	chains := map[string]bool{}
	residues := map[string]bool{}
	minX, minY, minZ := records[0].x, records[0].y, records[0].z
	maxX, maxY, maxZ := minX, minY, minZ
	for _, record := range records {
		if record.chain != "" {
			chains[record.chain] = true
		}
		residues[record.chain+":"+record.seq+":"+record.comp] = true
		minX = math.Min(minX, record.x)
		minY = math.Min(minY, record.y)
		minZ = math.Min(minZ, record.z)
		maxX = math.Max(maxX, record.x)
		maxY = math.Max(maxY, record.y)
		maxZ = math.Max(maxZ, record.z)
	}
	stats.Residues = len(residues)
	for chain := range chains {
		stats.Chains = append(stats.Chains, chain)
	}
	sort.Strings(stats.Chains)
	center := []float64{(minX + maxX) / 2, (minY + maxY) / 2, (minZ + maxZ) / 2}
	radius := 0.0
	for _, record := range records {
		dx := record.x - center[0]
		dy := record.y - center[1]
		dz := record.z - center[2]
		radius = math.Max(radius, math.Sqrt(dx*dx+dy*dy+dz*dz))
	}
	stats.BoundingBox = &boxReport{Min: []float64{minX, minY, minZ}, Max: []float64{maxX, maxY, maxZ}}
	stats.BoundingSphere = &sphere{Center: center, Radius: radius}
	return stats
}
