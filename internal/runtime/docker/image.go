package docker

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/winler/warden/internal/config"
	"github.com/winler/warden/internal/features"
)

// ImageTag computes the docker image tag for a base image + tool set.
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

// BaseImageTag returns the tag for the warden base image.
func BaseImageTag(base string) string {
	safe := strings.ReplaceAll(base, ":", "-")
	safe = strings.ReplaceAll(safe, "/", "-")
	return "warden:base-" + safe
}

// ImageExists checks if a docker image exists locally.
func ImageExists(tag string) (bool, error) {
	cmd := exec.Command("docker", "image", "inspect", tag)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil, nil
}

// BuildBaseImage ensures the warden base image exists. Builds if not cached.
func BuildBaseImage(base string) (string, error) {
	tag := BaseImageTag(base)
	exists, err := ImageExists(tag)
	if err != nil {
		return "", err
	}
	if exists {
		return tag, nil
	}

	fmt.Fprintf(os.Stderr, "warden: building base image (first run only)...\n")

	tmpDir, err := os.MkdirTemp("", "warden-base-*")
	if err != nil {
		return "", fmt.Errorf("creating build dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	dockerfile := BaseDockerfile(base)
	if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		return "", fmt.Errorf("writing Dockerfile: %w", err)
	}

	cmd := exec.Command("docker", "build", "-t", tag, tmpDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("building base image: %w", err)
	}

	return tag, nil
}

// BaseDockerfile returns the Dockerfile for the warden base image.
func BaseDockerfile(base string) string {
	return fmt.Sprintf(`FROM %s
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl wget ca-certificates openssh-client \
    git ripgrep fd-find tree less \
    build-essential pkg-config \
    jq unzip zip tar gzip \
    sudo locales \
    xvfb x11vnc x11-xserver-utils \
  && sed -i 's/# en_US.UTF-8/en_US.UTF-8/' /etc/locale.gen \
  && locale-gen \
  && rm -rf /var/lib/apt/lists/*
`, base)
}

// BuildImage creates a warden image with the specified tools installed.
func BuildImage(base string, tools []string) (string, error) {
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
	if err := os.MkdirAll(featDir, 0o755); err != nil {
		return "", fmt.Errorf("creating features dir: %w", err)
	}

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

// EnsureImage implements Runtime.EnsureImage for Docker.
func (d *DockerRuntime) EnsureImage(cfg config.SandboxConfig) (string, error) {
	if len(cfg.Tools) > 0 {
		return BuildImage(cfg.Image, cfg.Tools)
	}
	return BuildBaseImage(cfg.Image)
}
