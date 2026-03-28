package authbroker

import (
	"encoding/json"
	"testing"
)

func TestGenerateFakeCredentials(t *testing.T) {
	realJSON := []byte(`{
		"accessToken": "real-secret-token",
		"refreshToken": "real-refresh",
		"expiresAt": 1700000000000,
		"scopes": ["user:inference"],
		"subscriptionType": "max",
		"organizationId": "org-123",
		"accountId": "acc-456",
		"tokenType": "Bearer"
	}`)

	fake, err := GenerateFakeCredentials(realJSON, "")
	if err != nil {
		t.Fatalf("GenerateFakeCredentials: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(fake, &parsed); err != nil {
		t.Fatalf("unmarshal fake: %v", err)
	}

	if parsed["accessToken"] != FakeToken {
		t.Errorf("accessToken = %q, want %q", parsed["accessToken"], FakeToken)
	}
	if parsed["refreshToken"] != "warden-sandbox-refresh" {
		t.Errorf("refreshToken = %q", parsed["refreshToken"])
	}
	if parsed["expiresAt"].(float64) != 9999999999999 {
		t.Errorf("expiresAt = %v, want 9999999999999", parsed["expiresAt"])
	}

	if parsed["organizationId"] != "org-123" {
		t.Errorf("organizationId = %v, want org-123", parsed["organizationId"])
	}
	if parsed["accountId"] != "acc-456" {
		t.Errorf("accountId = %v, want acc-456", parsed["accountId"])
	}
	if parsed["subscriptionType"] != "max" {
		t.Errorf("subscriptionType = %v, want max", parsed["subscriptionType"])
	}
}

func TestGenerateFakeCredentialsInvalidJSON(t *testing.T) {
	_, err := GenerateFakeCredentials([]byte("not json"), "")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
