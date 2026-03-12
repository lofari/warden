package cli

import (
	"testing"

	"github.com/winler/warden/internal/runtime"
)

func TestListImagesNoDocker(t *testing.T) {
	// Verify AllRuntimes returns at least "docker" (registered via init import)
	runtimes := runtime.AllRuntimes()
	if len(runtimes) == 0 {
		t.Error("expected at least one registered runtime")
	}
}
