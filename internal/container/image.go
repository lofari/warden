package container

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/winler/warden/internal/features"
)

// ImageTag computes the docker image tag for a base image + tool set.
// If no tools, returns the base image tag.
func ImageTag(base string, tools []string) string {
	baseTag := BaseImageTag(base)
	if len(tools) == 0 {
		return baseTag
	}
	sorted := make([]string, len(tools))
	copy(sorted, tools)
	sort.Strings(sorted)
	safeName := strings.ReplaceAll(base, ":", "-")
	safeName = strings.ReplaceAll(safeName, "/", "-")
	return "warden:" + safeName + "_" + strings.Join(sorted, "_")
}

// ImageExists checks if a docker image exists locally.
func ImageExists(tag string) (bool, error) {
	cmd := exec.Command("docker", "image", "inspect", tag)
	cmd.Stdout = nil
	cmd.Stderr = nil
	err := cmd.Run()
	return err == nil, nil
}

// BuildImage creates a warden image with the specified tools installed.
// Always ensures the base image exists first. If no tools, returns the base image.
func BuildImage(base string, tools []string) (string, error) {
	// Ensure base image exists first
	baseTag, err := BuildBaseImage(base)
	if err != nil {
		return "", fmt.Errorf("building base image: %w", err)
	}

	tag := ImageTag(base, tools)
	if len(tools) == 0 {
		return baseTag, nil
	}

	exists, err := ImageExists(tag)
	if err != nil {
		return "", err
	}
	if exists {
		return tag, nil
	}

	tmpDir, err := os.MkdirTemp("", "warden-build-*")
	if err != nil {
		return "", fmt.Errorf("creating build dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	featDir := filepath.Join(tmpDir, "features")
	os.MkdirAll(featDir, 0o755)

	sorted := make([]string, len(tools))
	copy(sorted, tools)
	sort.Strings(sorted)

	var runLines []string
	for _, tool := range sorted {
		script, err := features.GetFeatureScript(tool)
		if err != nil {
			return "", fmt.Errorf("unknown tool %q: %w", tool, err)
		}
		scriptPath := filepath.Join(featDir, tool+".sh")
		if err := os.WriteFile(scriptPath, script, 0o755); err != nil {
			return "", fmt.Errorf("writing feature script: %w", err)
		}
		runLines = append(runLines, fmt.Sprintf("RUN /tmp/warden-features/%s.sh", tool))
	}

	// Use base image as FROM — base already has dev utilities installed
	dockerfile := fmt.Sprintf("FROM %s\nCOPY features/ /tmp/warden-features/\n%s\nRUN rm -rf /tmp/warden-features/\n",
		baseTag, strings.Join(runLines, "\n"))

	if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		return "", fmt.Errorf("writing Dockerfile: %w", err)
	}

	cmd := exec.Command("docker", "build", "-t", tag, tmpDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("building image: %w", err)
	}

	return tag, nil
}
