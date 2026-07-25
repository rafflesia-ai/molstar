package cli

import (
	"fmt"
	"math/bits"
	"strconv"
)

type visualGolden struct {
	Name                   string
	Width                  int
	Height                 int
	AverageHash            string
	MaxAverageHashDistance int
}

var demoVisualGolden = visualGolden{
	Name:                   "demo-96x72-spacefill",
	Width:                  96,
	Height:                 72,
	AverageHash:            "ffffe7c7c7c7ffff",
	MaxAverageHashDistance: 2,
}

func checkOutputReportAgainstGolden(report outputReport, golden visualGolden) error {
	if report.Width != golden.Width || report.Height != golden.Height {
		return fmt.Errorf("%s dimensions are %dx%d, expected %dx%d", golden.Name, report.Width, report.Height, golden.Width, golden.Height)
	}
	if report.AverageHash == "" {
		return fmt.Errorf("%s did not report an average hash", golden.Name)
	}
	distance, err := averageHashDistance(report.AverageHash, golden.AverageHash)
	if err != nil {
		return err
	}
	if distance > golden.MaxAverageHashDistance {
		return fmt.Errorf("%s average hash distance is %d, expected <= %d (got %s, expected %s)", golden.Name, distance, golden.MaxAverageHashDistance, report.AverageHash, golden.AverageHash)
	}
	return nil
}

func firstOutputReportFromParsed(report map[string]any) (outputReport, bool) {
	outputs, ok := report["output_files"].([]any)
	if !ok || len(outputs) == 0 {
		return outputReport{}, false
	}
	first, ok := outputs[0].(map[string]any)
	if !ok {
		return outputReport{}, false
	}
	return outputReportFromMap(first), true
}

func outputReportFromMap(value map[string]any) outputReport {
	report := outputReport{}
	if path, ok := value["path"].(string); ok {
		report.Path = path
	}
	if typ, ok := value["type"].(string); ok {
		report.Type = typ
	}
	if sha, ok := value["sha256"].(string); ok {
		report.SHA256 = sha
	}
	if hash, ok := value["average_hash"].(string); ok {
		report.AverageHash = hash
	}
	if width, ok := value["width"].(float64); ok {
		report.Width = int(width)
	}
	if height, ok := value["height"].(float64); ok {
		report.Height = int(height)
	}
	if verified, ok := value["verified"].(bool); ok {
		report.Verified = verified
	}
	if nonBlank, ok := value["non_blank"].(bool); ok {
		report.NonBlank = nonBlank
	}
	return report
}

func averageHashDistance(a string, b string) (int, error) {
	left, err := strconv.ParseUint(a, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("decode average hash %q: %w", a, err)
	}
	right, err := strconv.ParseUint(b, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("decode average hash %q: %w", b, err)
	}
	return bits.OnesCount64(left ^ right), nil
}
