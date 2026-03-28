package authbroker

import (
	"encoding/json"
	"fmt"
)

// FakeToken is the deterministic token value placed inside the sandbox.
const FakeToken = "warden-sandbox-token"

// GenerateFakeCredentials mirrors the structure of real credentials JSON,
// substituting only the token fields with fake values.
// If envelopeKey is non-empty, the result is wrapped in that key.
func GenerateFakeCredentials(realJSON []byte, envelopeKey string) ([]byte, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(realJSON, &raw); err != nil {
		return nil, fmt.Errorf("parsing credentials: %w", err)
	}

	raw["accessToken"] = FakeToken
	raw["refreshToken"] = "warden-sandbox-refresh"
	raw["expiresAt"] = float64(9999999999999)

	inner, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return nil, err
	}
	if envelopeKey == "" {
		return inner, nil
	}
	wrapped := map[string]json.RawMessage{envelopeKey: inner}
	return json.MarshalIndent(wrapped, "", "  ")
}
