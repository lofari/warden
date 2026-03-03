package config

type Mount struct {
	Path string `yaml:"path"`
	Mode string `yaml:"mode"` // "ro" or "rw"
}

type SandboxConfig struct {
	Image   string   `yaml:"image"`
	Tools   []string `yaml:"tools"`
	Mounts  []Mount  `yaml:"mounts"`
	Network bool     `yaml:"network"`
	Timeout string   `yaml:"timeout"`
	Memory  string   `yaml:"memory"`
	CPUs    int      `yaml:"cpus"`
	Workdir string   `yaml:"workdir"`
}
