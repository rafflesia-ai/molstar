package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rafflesia-ai/molstar/internal/job"
	"github.com/rafflesia-ai/molstar/internal/mvs"
	"github.com/rafflesia-ai/molstar/internal/render"
)

type outputReport struct {
	Path        string `json:"path"`
	Type        string `json:"type"`
	Bytes       int64  `json:"bytes,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	AverageHash string `json:"average_hash,omitempty"`
	Verified    bool   `json:"verified"`
	NonBlank    bool   `json:"non_blank,omitempty"`
	Temporary   bool   `json:"temporary,omitempty"`
	Atomic      bool   `json:"atomic,omitempty"`
}

func renderTransactional(ctx context.Context, runner render.ImageRenderer, scenePath string, output job.Output, saveMolj bool, stateOut string, dryRun bool) (render.CommandResult, []outputReport, error) {
	if dryRun {
		result, err := runner.RenderImage(ctx, render.ImageRequest{InputMVS: scenePath, Output: output, SaveMolj: saveMolj})
		reports := []outputReport{{Path: output.Path, Type: output.NormalizedType(), Verified: false}}
		if saveMolj {
			if stateOut == "" {
				stateOut = replaceExt(output.Path, ".molj")
			}
			reports = append(reports, outputReport{Path: stateOut, Type: "molj", Verified: false})
		}
		return result, reports, err
	}
	tmpOutput, cleanup, err := tempOutputPath(output.Path)
	if err != nil {
		return render.CommandResult{}, nil, err
	}
	defer cleanup()

	tmp := output
	tmp.Path = tmpOutput
	result, err := runner.RenderImage(ctx, render.ImageRequest{InputMVS: scenePath, Output: tmp, SaveMolj: saveMolj})
	if err != nil {
		return result, nil, err
	}
	report, err := verifyOutput(tmpOutput, output)
	if err != nil {
		return result, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(output.Path), 0o755); err != nil {
		return result, nil, err
	}
	if err := os.Rename(tmpOutput, output.Path); err != nil {
		return result, nil, err
	}
	report.Path = output.Path
	report.Atomic = true
	reports := []outputReport{report}
	if saveMolj {
		tmpState := replaceExt(tmpOutput, ".molj")
		if stateOut == "" {
			stateOut = replaceExt(output.Path, ".molj")
		}
		if err := os.MkdirAll(filepath.Dir(stateOut), 0o755); err != nil {
			return result, nil, err
		}
		if err := os.Rename(tmpState, stateOut); err != nil {
			return result, nil, err
		}
		stateInfo, err := os.Stat(stateOut)
		if err != nil {
			return result, nil, err
		}
		sha, err := fileSHA256(stateOut)
		if err != nil {
			return result, nil, err
		}
		reports = append(reports, outputReport{Path: stateOut, Type: "molj", Bytes: stateInfo.Size(), SHA256: sha, Verified: stateInfo.Size() > 0, Atomic: true})
	}
	return result, reports, nil
}

func writeMVSJTransactional(path string, document mvs.Document) (outputReport, error) {
	return writeExportTransactional(path, "mvsj", func(tmp string) error {
		return mvs.WriteFile(tmp, document)
	}, func(tmp string) error {
		data, err := os.ReadFile(tmp)
		if err != nil {
			return err
		}
		_, err = mvs.Decode(data)
		return err
	})
}

func writeMVSXTransactional(path string, j job.Job, document mvs.Document) (outputReport, error) {
	return writeExportTransactional(path, "mvsx", func(tmp string) error {
		return writeMVSXForJob(tmp, j, document)
	}, func(tmp string) error {
		return mvs.ValidateMVSX(tmp, job.ApplyRuntimeProfile(j.Runtime).MaxArchiveBytes)
	})
}

func writeReportTransactional(path string, data []byte) (outputReport, error) {
	return writeExportTransactional(path, "report", func(tmp string) error {
		return os.WriteFile(tmp, data, 0o644)
	}, func(tmp string) error {
		info, err := os.Stat(tmp)
		if err != nil {
			return err
		}
		if info.Size() == 0 {
			return fmt.Errorf("report %s is empty", tmp)
		}
		return nil
	})
}

func writeExportTransactional(path string, outputType string, write func(string) error, verify func(string) error) (outputReport, error) {
	tmpOutput, cleanup, err := tempOutputPath(path)
	if err != nil {
		return outputReport{}, err
	}
	defer cleanup()
	if err := write(tmpOutput); err != nil {
		return outputReport{}, err
	}
	if err := verify(tmpOutput); err != nil {
		return outputReport{}, err
	}
	report, err := exportedFileReport(tmpOutput, outputType)
	if err != nil {
		return outputReport{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return outputReport{}, err
	}
	if err := os.Rename(tmpOutput, path); err != nil {
		return outputReport{}, err
	}
	report.Path = path
	report.Atomic = true
	return report, nil
}

func tempOutputPath(path string) (string, func(), error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, err
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), ext)
	file, err := os.CreateTemp(dir, "."+base+".*"+ext)
	if err != nil {
		return "", nil, err
	}
	tmp := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", nil, err
	}
	_ = os.Remove(tmp)
	return tmp, func() {
		_ = os.Remove(tmp)
		_ = os.Remove(replaceExt(tmp, ".molj"))
	}, nil
}

func verifyOutput(path string, output job.Output) (outputReport, error) {
	info, err := os.Stat(path)
	if err != nil {
		return outputReport{}, err
	}
	sha, err := fileSHA256(path)
	if err != nil {
		return outputReport{}, err
	}
	report := outputReport{Path: path, Type: output.NormalizedType(), Bytes: info.Size(), SHA256: sha}
	if info.Size() == 0 {
		return report, fmt.Errorf("output %s is empty", path)
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg":
		file, err := os.Open(path)
		if err != nil {
			return report, err
		}
		img, _, err := image.Decode(file)
		_ = file.Close()
		if err != nil {
			return report, err
		}
		bounds := img.Bounds()
		report.Width = bounds.Dx()
		report.Height = bounds.Dy()
		expectedWidth, expectedHeight := output.SizeOrDefault(800, 800)
		if report.Width != expectedWidth || report.Height != expectedHeight {
			return report, fmt.Errorf("output %s dimensions are %dx%d, expected %dx%d", path, report.Width, report.Height, expectedWidth, expectedHeight)
		}
		report.NonBlank = imageNonBlank(img)
		if !report.NonBlank {
			// A blank image means the scene rendered nothing visible: the renderer
			// itself worked. Classify it as a scene problem so agents fix selectors
			// or the camera instead of retrying an identical job or running doctor.
			return report, markError(kindInvalidScene, fmt.Errorf("output %s appears blank: the scene rendered no visible geometry", output.Path))
		}
		report.AverageHash = imageAverageHash(img)
	}
	report.Verified = true
	return report, nil
}

func exportedFileReport(path string, outputType string) (outputReport, error) {
	info, err := os.Stat(path)
	if err != nil {
		return outputReport{}, err
	}
	sha, err := fileSHA256(path)
	if err != nil {
		return outputReport{}, err
	}
	return outputReport{
		Path:     path,
		Type:     outputType,
		Bytes:    info.Size(),
		SHA256:   sha,
		Verified: info.Size() > 0,
	}, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func imageAverageHash(img image.Image) string {
	bounds := img.Bounds()
	var values [64]uint32
	var total uint64
	for y := 0; y < 8; y++ {
		sourceY := bounds.Min.Y + (y*bounds.Dy()+bounds.Dy()/2)/8
		if sourceY >= bounds.Max.Y {
			sourceY = bounds.Max.Y - 1
		}
		for x := 0; x < 8; x++ {
			sourceX := bounds.Min.X + (x*bounds.Dx()+bounds.Dx()/2)/8
			if sourceX >= bounds.Max.X {
				sourceX = bounds.Max.X - 1
			}
			r, g, b, _ := img.At(sourceX, sourceY).RGBA()
			gray := (299*r + 587*g + 114*b) / 1000
			index := y*8 + x
			values[index] = gray
			total += uint64(gray)
		}
	}
	average := total / 64
	var bits uint64
	for i, value := range values {
		if uint64(value) >= average {
			bits |= 1 << uint(63-i)
		}
	}
	return fmt.Sprintf("%016x", bits)
}

func imageNonBlank(img image.Image) bool {
	bounds := img.Bounds()
	var first [4]uint32
	seen := false
	samples := 0
	stepX := max(1, bounds.Dx()/32)
	stepY := max(1, bounds.Dy()/32)
	for y := bounds.Min.Y; y < bounds.Max.Y; y += stepY {
		for x := bounds.Min.X; x < bounds.Max.X; x += stepX {
			r, g, b, a := img.At(x, y).RGBA()
			current := [4]uint32{r, g, b, a}
			if !seen {
				first = current
				seen = true
			} else if current != first {
				return true
			}
			if a != 0 && (r != 0 || g != 0 || b != 0) {
				samples++
			}
		}
	}
	return samples > 0 && !allWhiteOrTransparent(first)
}

func allWhiteOrTransparent(pixel [4]uint32) bool {
	return pixel[3] == 0 || (pixel[0] == 0xffff && pixel[1] == 0xffff && pixel[2] == 0xffff)
}
