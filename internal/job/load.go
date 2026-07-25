package job

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func LoadFile(path string) (Job, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Job{}, err
	}
	return LoadBytes(data, path)
}

func LoadRenderFile(path string) (Job, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Job{}, err
	}
	return LoadRenderBytes(data, path)
}

func LoadMany(path string) ([]Job, error) {
	if strings.EqualFold(filepath.Ext(path), ".jsonl") {
		return loadJSONL(path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return LoadManyBytes(data, path)
}

func LoadBytes(data []byte, name string) (Job, error) {
	j, err := Decode(data, name)
	if err != nil {
		return Job{}, err
	}
	return j, j.ValidateScene()
}

func LoadRenderBytes(data []byte, name string) (Job, error) {
	j, err := LoadBytes(data, name)
	if err != nil {
		return Job{}, err
	}
	return j, j.ValidateRender()
}

func LoadStrictBytes(data []byte, name string) (Job, error) {
	var j Job
	if isJSONPath(name) || json.Valid(data) {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&j); err != nil {
			return Job{}, fmt.Errorf("decode strict json: %w", err)
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			return Job{}, fmt.Errorf("decode strict json: trailing data")
		}
	} else {
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(true)
		if err := decoder.Decode(&j); err != nil {
			return Job{}, fmt.Errorf("decode strict yaml: %w", err)
		}
	}
	return j, j.ValidateRender()
}

func LoadManyBytes(data []byte, name string) ([]Job, error) {
	var jobs []Job
	if looksLikeJSONL(data) {
		if jsonl, err := decodeJSONLBytes(data, name); err == nil {
			return validateMany(jsonl)
		}
	}
	if isJSONPath(name) || json.Valid(data) {
		if err := json.Unmarshal(data, &jobs); err == nil && len(jobs) > 0 {
			return validateMany(jobs)
		}
	} else {
		if err := yaml.Unmarshal(data, &jobs); err == nil && len(jobs) > 0 {
			return validateMany(jobs)
		}
	}
	j, err := Decode(data, name)
	if err != nil {
		if jsonl, jsonlErr := decodeJSONLBytes(data, name); jsonlErr == nil {
			return validateMany(jsonl)
		}
		return nil, err
	}
	return validateMany([]Job{j})
}

func Decode(data []byte, name string) (Job, error) {
	var j Job
	if isJSONPath(name) || json.Valid(data) {
		if err := json.Unmarshal(data, &j); err != nil {
			return Job{}, fmt.Errorf("decode json: %w", err)
		}
		return j, nil
	}
	if err := yaml.Unmarshal(data, &j); err != nil {
		return Job{}, fmt.Errorf("decode yaml: %w", err)
	}
	return j, nil
}

func loadJSONL(path string) ([]Job, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var jobs []Job
	scanner := bufio.NewScanner(file)
	line := 0
	for scanner.Scan() {
		line++
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		var j Job
		if err := json.Unmarshal(raw, &j); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		jobs = append(jobs, j)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, fmt.Errorf("%s does not contain any jobs", path)
	}
	return validateMany(jobs)
}

func decodeJSONLBytes(data []byte, name string) ([]Job, error) {
	var jobs []Job
	scanner := bufio.NewScanner(bytes.NewReader(data))
	line := 0
	for scanner.Scan() {
		line++
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		var j Job
		if err := json.Unmarshal(raw, &j); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", name, line, err)
		}
		jobs = append(jobs, j)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, fmt.Errorf("%s does not contain any jobs", name)
	}
	return jobs, nil
}

func looksLikeJSONL(data []byte) bool {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	objects := 0
	for scanner.Scan() {
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		if raw[0] != '{' {
			return false
		}
		objects++
	}
	return objects > 1
}

func validateMany(jobs []Job) ([]Job, error) {
	for i, j := range jobs {
		if err := j.ValidateRender(); err != nil {
			return nil, fmt.Errorf("job %d: %w", i, err)
		}
	}
	return jobs, nil
}

func isJSONPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".json" || ext == ".jsonl"
}
