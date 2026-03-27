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
	Proxy   []string `yaml:"proxy"`
}

type WardenFile struct {
	Default  ProfileEntry            `yaml:"default"`
	Profiles map[string]ProfileEntry `yaml:"profiles"`
}

func ParseWardenYAML(data []byte) (*WardenFile, error) {
	var file WardenFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parsing .warden.yaml: %w", err)
	}
	return &file, nil
}
