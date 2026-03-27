package config

type Mount struct {
	Path         string   `yaml:"path"`
	Mode         string   `yaml:"mode"`          // "ro" or "rw"
	DenyExtra    []string `yaml:"deny_extra"`    // additional deny patterns (added to defaults)
	DenyOverride []string `yaml:"deny_override"` // replaces default deny patterns entirely
	ReadOnly     []string `yaml:"read_only"`     // paths that are read-only within this mount
}

type SandboxConfig struct {
	Runtime string   `yaml:"runtime"`
	Image   string   `yaml:"image"`
	Tools   []string `yaml:"tools"`
	Mounts  []Mount  `yaml:"mounts"`
	Network bool     `yaml:"network"`
	Timeout string   `yaml:"timeout"`
	Memory  string   `yaml:"memory"`
	CPUs    int      `yaml:"cpus"`
	Workdir string   `yaml:"workdir"`
	Env     []string `yaml:"env"`
	Proxy   []string `yaml:"proxy"`
	Display    bool              `yaml:"display"`
	Resolution string            `yaml:"resolution"`
	AuthBroker *AuthBrokerConfig `yaml:"auth_broker,omitempty"`
}

type AuthBrokerConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Credentials string `yaml:"credentials,omitempty"`
	Target      string `yaml:"target,omitempty"`
}
