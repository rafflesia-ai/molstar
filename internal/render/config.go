package render

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const ConfigEnv = "MOLSTAR_CONFIG"

type RuntimeConfig struct {
	Home                    string   `json:"home,omitempty"`
	RendererCommand         []string `json:"renderer_command,omitempty"`
	RendererFallbackCommand []string `json:"renderer_fallback_command,omitempty"`
	ValidateCommand         []string `json:"validate_command,omitempty"`
}

func LoadRuntimeConfig() (RuntimeConfig, string, error) {
	path, err := DefaultConfigPath()
	if err != nil {
		return RuntimeConfig{}, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return RuntimeConfig{}, path, err
	}
	var config RuntimeConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return RuntimeConfig{}, path, err
	}
	config.Home = strings.TrimSpace(config.Home)
	return config, path, nil
}

func WriteRuntimeConfig(path string, config RuntimeConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func DefaultConfigPath() (string, error) {
	if path := strings.TrimSpace(os.Getenv(ConfigEnv)); path != "" {
		return path, nil
	}
	configHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "molstar", "config.json"), nil
}

func RuntimeConfigForHome(home string) RuntimeConfig {
	node := filepath.Join(home, "node_modules", "node", "bin", "node")
	if info, err := os.Stat(node); err != nil || info.IsDir() {
		node = "node"
	}
	renderer := filepath.Join(home, "scripts", "render-mvs.js")
	wrapper := filepath.Join(home, "scripts", "molstar-node-cli.js")
	mvsRender := filepath.Join(home, "node_modules", ".bin", "mvs-render")
	mvsValidate := filepath.Join(home, "node_modules", ".bin", "mvs-validate")
	return RuntimeConfig{
		Home:                    home,
		RendererCommand:         []string{node, renderer},
		RendererFallbackCommand: []string{node, wrapper, mvsRender},
		ValidateCommand:         []string{node, wrapper, mvsValidate},
	}
}

func validCommand(command []string) []string {
	if Available(command) {
		return command
	}
	return nil
}
