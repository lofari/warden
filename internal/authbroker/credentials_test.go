package authbroker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewCredentialStore(t *testing.T) {
	dir := t.TempDir()
	credsPath := filepath.Join(dir, "credentials.json")

	creds := map[string]interface{}{
		"accessToken":      "real-access-token",
		"refreshToken":     "real-refresh-token",
		"expiresAt":        float64(time.Now().Add(1 * time.Hour).UnixMilli()),
		"scopes":           []string{"user:inference"},
		"subscriptionType": "max",
		"tokenType":        "Bearer",
	}
	data, _ := json.Marshal(creds)
	os.WriteFile(credsPath, data, 0o600)

	store, err := NewCredentialStore(credsPath)
	if err != nil {
		t.Fatalf("NewCredentialStore: %v", err)
	}

	token, err := store.GetAccessToken()
	if err != nil {
		t.Fatalf("GetAccessToken: %v", err)
	}
	if token != "real-access-token" {
		t.Errorf("token = %q, want %q", token, "real-access-token")
	}
	if store.SubscriptionType() != "max" {
		t.Errorf("subType = %q, want %q", store.SubscriptionType(), "max")
	}
}

func TestCredentialStoreFileNotFound(t *testing.T) {
	_, err := NewCredentialStore("/nonexistent/path/credentials.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestCredentialStoreExpiredToken(t *testing.T) {
	dir := t.TempDir()
	credsPath := filepath.Join(dir, "credentials.json")

	creds := map[string]interface{}{
		"accessToken":      "expired-token",
		"refreshToken":     "refresh-token",
		"expiresAt":        float64(time.Now().Add(-1 * time.Hour).UnixMilli()),
		"scopes":           []string{"user:inference"},
		"subscriptionType": "pro",
	}
	data, _ := json.Marshal(creds)
	os.WriteFile(credsPath, data, 0o600)

	store, err := NewCredentialStore(credsPath)
	if err != nil {
		t.Fatalf("NewCredentialStore: %v", err)
	}

	// GetAccessToken should detect expiry and attempt refresh.
	// Without a real OAuth server, refresh will fail.
	_, err = store.GetAccessToken()
	if err == nil {
		t.Fatal("expected error when refresh fails (no OAuth server)")
	}
}

func TestCredentialStoreRawJSON(t *testing.T) {
	dir := t.TempDir()
	credsPath := filepath.Join(dir, "credentials.json")

	creds := map[string]interface{}{
		"accessToken":      "tok",
		"refreshToken":     "ref",
		"expiresAt":        float64(time.Now().Add(1 * time.Hour).UnixMilli()),
		"scopes":           []string{"user:inference"},
		"subscriptionType": "max",
		"organizationId":   "org-123",
		"accountId":        "acc-456",
	}
	data, _ := json.Marshal(creds)
	os.WriteFile(credsPath, data, 0o600)

	store, err := NewCredentialStore(credsPath)
	if err != nil {
		t.Fatalf("NewCredentialStore: %v", err)
	}

	raw := store.RawJSON()
	var parsed map[string]interface{}
	json.Unmarshal(raw, &parsed)
	if parsed["organizationId"] != "org-123" {
		t.Errorf("missing organizationId in raw JSON")
	}
}
