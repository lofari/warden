package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/winler/warden/internal/config"
	"github.com/winler/warden/internal/runtime"
	docker "github.com/winler/warden/internal/runtime/docker"
	"github.com/winler/warden/internal/runtime/shared"
)

// SandboxRuntime implements runtime.Runtime using Docker Desktop Sandboxes.
type SandboxRuntime struct{}

func init() {
	runtime.Register("sandbox", func() runtime.Runtime {
		return &SandboxRuntime{}
	})
}

// Preflight verifies docker sandbox CLI is available.
func (s *SandboxRuntime) Preflight() error {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("warden: docker is not installed")
	}
	out, err := exec.Command(dockerPath, "sandbox", "version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("warden: docker sandbox not available (requires Docker Desktop): %s",
			strings.TrimSpace(string(out)))
	}
	return nil
}

// EnsureImage builds the warden image locally and creates the sandbox if needed.
func (s *SandboxRuntime) EnsureImage(cfg config.SandboxConfig) (string, error) {
	// Build warden image on host using existing Docker image logic
	var imageTag string
	var err error
	if len(cfg.Tools) > 0 {
		imageTag, err = docker.BuildImage(cfg.Image, cfg.Tools)
	} else {
		imageTag, err = docker.BuildBaseImage(cfg.Image)
	}
	if err != nil {
		return "", err
	}

	// Determine workspace path (first rw mount)
	workdir := cfg.Workdir
	if workdir == "" {
		for _, m := range cfg.Mounts {
			if m.Mode == "rw" {
				workdir = m.Path
				break
			}
		}
	}
	if workdir == "" {
		return "", fmt.Errorf("warden: no workspace directory for sandbox")
	}

	name := SandboxName(workdir)

	// Check for image mismatch on existing sandbox
	if sandboxExists(name) {
		existing := inspectTemplate(name)
		if existing != "" && existing != imageTag {
			fmt.Fprintf(os.Stderr, "warden: sandbox %s was created with %s but current config needs %s\n"+
				"warden: run 'warden rm' to recreate with the new image\n", name, existing, imageTag)
		}
		return imageTag, nil
	}

	// Create sandbox with warden image as template
	fmt.Fprintf(os.Stderr, "warden: creating sandbox %s...\n", name)
	if err := ensureSandbox(name, imageTag, workdir); err != nil {
		return "", err
	}

	return imageTag, nil
}

// DryRun prints the docker sandbox commands that would be executed.
func (s *SandboxRuntime) DryRun(cfg config.SandboxConfig, command []string) error {
	imageTag := docker.ImageTag(cfg.Image, cfg.Tools)

	workdir := cfg.Workdir
	if workdir == "" {
		for _, m := range cfg.Mounts {
			if m.Mode == "rw" {
				workdir = m.Path
				break
			}
		}
	}

	name := SandboxName(workdir)

	fmt.Printf("# Create sandbox (if not exists):\n")
	fmt.Printf("docker sandbox create --template %s --name %s shell %s\n\n", imageTag, name, workdir)
	fmt.Printf("# Execute command:\n")
	args := buildExecArgs(cfg, name, command, shared.IsTerminal())
	fmt.Printf("docker %s\n", strings.Join(args, " "))

	if cfg.Ephemeral {
		fmt.Printf("\n# Cleanup (ephemeral mode):\n")
		fmt.Printf("docker sandbox rm %s\n", name)
	}
	return nil
}

// ListImages delegates to Docker image listing (images are on host).
func (s *SandboxRuntime) ListImages() ([]runtime.ImageInfo, error) {
	d := &docker.DockerRuntime{}
	images, err := d.ListImages()
	if err != nil {
		return nil, err
	}
	// Re-tag as sandbox runtime
	for i := range images {
		images[i].Runtime = "sandbox"
	}
	return images, nil
}

// PruneImages delegates to Docker image pruning (images are on host).
func (s *SandboxRuntime) PruneImages() error {
	d := &docker.DockerRuntime{}
	return d.PruneImages()
}

// ListRunning returns currently running warden sandboxes.
func (s *SandboxRuntime) ListRunning() ([]runtime.RunningInstance, error) {
	return listSandboxes()
}

// Run executes a command in the sandbox. Implemented in run.go.
func (s *SandboxRuntime) Run(cfg config.SandboxConfig, command []string) (int, error) {
	return 1, fmt.Errorf("warden: sandbox Run not yet implemented")
}

// Stop halts a running sandbox by name. Implemented in run.go.
func (s *SandboxRuntime) Stop(name string) error {
	return stopSandbox(name)
}

// Remove deletes a stopped sandbox and reclaims resources. Implemented in run.go.
func (s *SandboxRuntime) Remove(name string) error {
	return removeSandbox(name)
}
