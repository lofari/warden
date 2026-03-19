package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// GlobalConfig holds user-level settings from ~/.warden/config.yaml.
type GlobalConfig struct {
	Firecracker FirecrackerGlobalConfig `yaml:"firecracker"`
}

// FirecrackerGlobalConfig holds Firecracker-specific global settings.
type FirecrackerGlobalConfig struct {
	Kernel string `yaml:"kernel"`
}

// LoadGlobalConfig reads the global config from the given path.
// Returns zero-value config if the file doesn't exist.
func LoadGlobalConfig(path string) (GlobalConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return GlobalConfig{}, nil
		}
		return GlobalConfig{}, fmt.Errorf("reading global config: %w", err)
	}

	var cfg GlobalConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return GlobalConfig{}, fmt.Errorf("parsing global config: %w", err)
	}
	return cfg, nil
}
