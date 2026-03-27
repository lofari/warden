package proxy

// ProxyHandshake is sent by the shim to the host after connecting.
type ProxyHandshake struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	TTY     bool     `json:"tty"`
	Env     []string `json:"env,omitempty"`
}

// ProxyReady is sent by the host to acknowledge the handshake.
type ProxyReady struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

