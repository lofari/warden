package authbroker

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBrokerRejectsWrongToken(t *testing.T) {
	broker := newTestBroker(t, "https://example.com")
	defer broker.Close()

	req, _ := http.NewRequest("POST", brokerURL(broker)+"/v1/messages", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer wrong-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestBrokerRejectsDisallowedPath(t *testing.T) {
	broker := newTestBroker(t, "https://example.com")
	defer broker.Close()

	req, _ := http.NewRequest("GET", brokerURL(broker)+"/v1/api_keys", nil)
	req.Header.Set("Authorization", "Bearer "+FakeToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestBrokerForwardsAllowedPath(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"auth": auth})
	}))
	defer upstream.Close()

	broker := newTestBrokerWithTransport(t, upstream.URL, upstream.Client().Transport)
	defer broker.Close()

	req, _ := http.NewRequest("POST", brokerURL(broker)+"/v1/messages", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+FakeToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if !strings.HasPrefix(result["auth"], "Bearer real-") {
		t.Errorf("upstream auth = %q, want real token injected", result["auth"])
	}
}

func TestBrokerAllowedPaths(t *testing.T) {
	allowed := []string{"/v1/messages", "/v1/complete", "/api/oauth/file_upload"}
	for _, path := range allowed {
		t.Run(path, func(t *testing.T) {
			upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer upstream.Close()

			broker := newTestBrokerWithTransport(t, upstream.URL, upstream.Client().Transport)
			defer broker.Close()

			req, _ := http.NewRequest("POST", brokerURL(broker)+path, strings.NewReader("{}"))
			req.Header.Set("Authorization", "Bearer "+FakeToken)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("status = %d, want 200 for allowed path %s", resp.StatusCode, path)
			}
		})
	}
}

func newTestBroker(t *testing.T, target string) *Broker {
	return newTestBrokerWithTransport(t, target, nil)
}

func newTestBrokerWithTransport(t *testing.T, target string, transport http.RoundTripper) *Broker {
	t.Helper()
	dir := t.TempDir()
	credsPath := filepath.Join(dir, "credentials.json")
	creds := map[string]interface{}{
		"accessToken":      "real-access-token",
		"refreshToken":     "real-refresh-token",
		"expiresAt":        float64(time.Now().Add(1 * time.Hour).UnixMilli()),
		"scopes":           []string{"user:inference"},
		"subscriptionType": "max",
	}
	data, _ := json.Marshal(creds)
	os.WriteFile(credsPath, data, 0o600)

	store, err := NewCredentialStore(credsPath)
	if err != nil {
		t.Fatalf("NewCredentialStore: %v", err)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	broker := NewBroker(store, target, l, transport)
	go broker.Serve()
	return broker
}

func brokerURL(b *Broker) string {
	return "http://" + b.Listener.Addr().String()
}
