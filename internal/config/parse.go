package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

type ProfileEntry struct {
	Extends string   `yaml:"extends"`
	Runtime *string  `yaml:"runtime"`
	Image   *string  `yaml:"image"`
	Tools   []string `yaml:"tools"`
	Mounts  []Mount  `yaml:"mounts"`
	Network *bool    `yaml:"network"`
	Timeout *string  `yaml:"timeout"`
	Memory  *string  `yaml:"memory"`
	CPUs    *int     `yaml:"cpus"`
	Workdir *string  `yaml:"workdir"`
	Env     []string `yaml:"env"`
	Proxy      []string          `yaml:"proxy"`
	AuthBroker *AuthBrokerConfig `yaml:"auth_broker,omitempty"`
	Ephemeral  *bool             `yaml:"ephemeral,omitempty"`
}

type WardenFile struct {
	Default   ProfileEntry            `yaml:"default"`
	Profiles  map[string]ProfileEntry `yaml:"profiles"`
	Ephemeral *bool                   `yaml:"ephemeral,omitempty"`
}

func ParseWardenYAML(data []byte) (*WardenFile, error) {
	var file WardenFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing .warden.yaml: %w", err)
	}
	// Propagate top-level shorthand fields into Default if not already set.
	if file.Ephemeral != nil && file.Default.Ephemeral == nil {
		file.Default.Ephemeral = file.Ephemeral
	}
	return &file, nil
}
