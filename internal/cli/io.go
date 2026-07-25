package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func (a app) readInput(path string) ([]byte, string, error) {
	if path == "-" {
		data, err := io.ReadAll(a.stdin)
		if err != nil {
			return nil, "", err
		}
		return data, "stdin", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	return data, path, nil
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func writeYAML(w io.Writer, value any) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func writeBytesPath(stdout io.Writer, path string, data []byte) error {
	if path == "" || path == "-" {
		_, err := stdout.Write(data)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func exactArgs(args []string, count int, usage string) error {
	if len(args) != count {
		return fmt.Errorf("%s expects %d argument(s), got %d", usage, count, len(args))
	}
	return nil
}
