package authbroker

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

// CredentialStore reads and refreshes OAuth credentials from the host filesystem.
type CredentialStore struct {
	path         string
	mu           sync.Mutex
	accessToken  string
	refreshToken string
	expiresAt    time.Time
	subType      string
	rawData      json.RawMessage
	envelopeKey  string // non-empty if credentials were nested (e.g. "claudeAiOauth")
}

// NewCredentialStore reads credentials from the given path.
func NewCredentialStore(path string) (*CredentialStore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading credentials: %w", err)
	}

	// Claude stores credentials either at the top level or nested under
	// a "claudeAiOauth" key. Unwrap the envelope if needed.
	innerData, envelopeKey := unwrapCredentials(data)

	var parsed struct {
		AccessToken      string   `json:"accessToken"`
		RefreshToken     string   `json:"refreshToken"`
		ExpiresAt        float64  `json:"expiresAt"`
		SubscriptionType string   `json:"subscriptionType"`
		Scopes           []string `json:"scopes"`
	}
	if err := json.Unmarshal(innerData, &parsed); err != nil {
		return nil, fmt.Errorf("parsing credentials: %w", err)
	}
	if parsed.AccessToken == "" {
		return nil, fmt.Errorf("credentials file missing accessToken")
	}

	return &CredentialStore{
		path:         path,
		accessToken:  parsed.AccessToken,
		refreshToken: parsed.RefreshToken,
		expiresAt:    time.UnixMilli(int64(parsed.ExpiresAt)),
		subType:      parsed.SubscriptionType,
		rawData:      json.RawMessage(innerData),
		envelopeKey:  envelopeKey,
	}, nil
}

// GetAccessToken returns a valid access token, refreshing if expired.
func (c *CredentialStore) GetAccessToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if time.Now().Before(c.expiresAt.Add(-30 * time.Second)) {
		return c.accessToken, nil
	}

	if err := c.refreshLocked(); err != nil {
		return "", fmt.Errorf("token refresh failed: %w", err)
	}
	return c.accessToken, nil
}

// SubscriptionType returns the subscription type (e.g. "max", "pro").
func (c *CredentialStore) SubscriptionType() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.subType
}

// RawJSON returns the full raw credentials JSON for structure mirroring.
func (c *CredentialStore) RawJSON() json.RawMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make(json.RawMessage, len(c.rawData))
	copy(cp, c.rawData)
	return cp
}

// EnvelopeKey returns the envelope key if credentials were nested (e.g. "claudeAiOauth").
func (c *CredentialStore) EnvelopeKey() string {
	return c.envelopeKey
}

// refreshLocked performs the OAuth token refresh. Caller must hold c.mu.
func (c *CredentialStore) refreshLocked() error {
	if c.refreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	resp, err := http.PostForm("https://api.anthropic.com/oauth/token", url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {c.refreshToken},
	})
	if err != nil {
		return fmt.Errorf("refresh request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("refresh returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		AccessToken  string  `json:"access_token"`
		RefreshToken string  `json:"refresh_token"`
		ExpiresIn    float64 `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parsing refresh response: %w", err)
	}

	c.accessToken = result.AccessToken
	if result.RefreshToken != "" {
		c.refreshToken = result.RefreshToken
	}
	c.expiresAt = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)

	var rawMap map[string]interface{}
	if err := json.Unmarshal(c.rawData, &rawMap); err != nil {
		fmt.Fprintf(os.Stderr, "warden: failed to parse raw credentials for writeback: %v\n", err)
		return nil
	}
	rawMap["accessToken"] = c.accessToken
	rawMap["refreshToken"] = c.refreshToken
	rawMap["expiresAt"] = float64(c.expiresAt.UnixMilli())
	updated, _ := json.MarshalIndent(rawMap, "", "  ")
	c.rawData = json.RawMessage(updated)
	fileData := updated
	if c.envelopeKey != "" {
		wrapped := map[string]json.RawMessage{c.envelopeKey: updated}
		fileData, _ = json.MarshalIndent(wrapped, "", "  ")
	}
	if err := os.WriteFile(c.path, fileData, 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "warden: failed to write refreshed credentials to disk: %v\n", err)
	}

	return nil
}

// unwrapCredentials checks if the JSON has a "claudeAiOauth" envelope and
// extracts the inner object. Returns the inner data and the envelope key name.
// If no envelope is found, returns the original data and an empty key.
func unwrapCredentials(data []byte) ([]byte, string) {
	var envelope struct {
		ClaudeAiOauth json.RawMessage `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &envelope); err == nil && len(envelope.ClaudeAiOauth) > 0 {
		return []byte(envelope.ClaudeAiOauth), "claudeAiOauth"
	}
	return data, ""
}
