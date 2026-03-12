package firecracker

import "testing"

func TestPreflightNoKVM(t *testing.T) {
	rt := &FirecrackerRuntime{}
	err := rt.Preflight()
	if err == nil {
		t.Skip("KVM is available, cannot test missing KVM path")
	}
	t.Logf("preflight error (expected): %v", err)
}
