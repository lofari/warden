package authbroker

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
)

// AllowedPaths is the set of API paths the broker will forward.
var AllowedPaths = []string{
	"/v1/messages",
	"/v1/complete",
	"/api/oauth/file_upload",
}

// Broker is a reverse proxy that validates fake tokens and injects real ones.
type Broker struct {
	Credentials *CredentialStore
	Target      string
	Listener    net.Listener
	server      *http.Server
}

// NewBroker creates a broker. If transport is non-nil, it's used for upstream requests.
func NewBroker(creds *CredentialStore, target string, listener net.Listener, transport http.RoundTripper) *Broker {
	b := &Broker{
		Credentials: creds,
		Target:      target,
		Listener:    listener,
	}

	targetURL, _ := url.Parse(target)

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = targetURL.Scheme
			req.URL.Host = targetURL.Host
			req.Host = targetURL.Host
		},
	}
	if transport != nil {
		proxy.Transport = transport
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// 1. Validate fake token
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+FakeToken {
			http.Error(w, "forbidden: invalid token", http.StatusForbidden)
			return
		}

		// 2. Check path allowlist
		if !isAllowedPath(r.URL.Path) {
			fmt.Fprintf(os.Stderr, "warden: auth broker rejected path: %s\n", r.URL.Path)
			http.Error(w, "forbidden: path not allowed", http.StatusForbidden)
			return
		}

		// 3. Get real token
		realToken, err := creds.GetAccessToken()
		if err != nil {
			http.Error(w, "bad gateway: "+err.Error(), http.StatusBadGateway)
			return
		}

		// 4. Replace authorization header
		r.Header.Set("Authorization", "Bearer "+realToken)

		// 5. Forward to upstream
		proxy.ServeHTTP(w, r)
	})

	b.server = &http.Server{Handler: mux}
	return b
}

// Serve starts accepting connections.
func (b *Broker) Serve() error {
	return b.server.Serve(b.Listener)
}

// Close shuts down the broker.
func (b *Broker) Close() error {
	return b.server.Close()
}

func isAllowedPath(path string) bool {
	for _, allowed := range AllowedPaths {
		if strings.HasPrefix(path, allowed) {
			return true
		}
	}
	return false
}
