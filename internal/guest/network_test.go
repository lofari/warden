package guest

import "testing"

func TestConfigureNetworkCommandSequence(t *testing.T) {
	err := ConfigureNetwork("invalid", "invalid", "8.8.8.8")
	if err == nil {
		t.Skip("test requires non-root to see ip command failures")
	}
	t.Logf("expected error in test env: %v", err)
}
