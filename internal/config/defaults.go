package config

import "runtime"

func DefaultConfig() SandboxConfig {
	return SandboxConfig{
		Runtime: "docker",
		Image:   "ubuntu:24.04",
		Network: false,
		Memory:  "8g",
		CPUs:    runtime.NumCPU(),
	}
}
