package cli

import (
	"encoding/json"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func marshalJSON(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func marshalYAML(value any) ([]byte, error) {
	return yaml.Marshal(value)
}

func writeJSONFile(path string, value any) error {
	data, err := marshalJSON(value)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
