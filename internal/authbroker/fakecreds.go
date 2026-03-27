package authbroker

import (
	"encoding/json"
	"fmt"
)

// FakeToken is the deterministic token value placed inside the sandbox.
const FakeToken = "warden-sandbox-token"

// GenerateFakeCredentials mirrors the structure of real credentials JSON,
// substituting only the token fields with fake values.
func GenerateFakeCredentials(realJSON []byte) ([]byte, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(realJSON, &raw); err != nil {
		return nil, fmt.Errorf("parsing credentials: %w", err)
	}

	raw["accessToken"] = FakeToken
	raw["refreshToken"] = "warden-sandbox-refresh"
	raw["expiresAt"] = float64(9999999999999)

	return json.MarshalIndent(raw, "", "  ")
}
