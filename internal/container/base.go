package container

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BaseImageTag returns the tag for the warden base image derived from a given base.
func BaseImageTag(base string) string {
	safe := strings.ReplaceAll(base, ":", "-")
	safe = strings.ReplaceAll(safe, "/", "-")
	return "warden:base-" + safe
}

// BuildBaseImage ensures the warden base image exists for the given base image.
// Returns the base image tag. Builds if not cached.
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

// BaseDockerfile returns the Dockerfile content for building the warden base image.
func BaseDockerfile(base string) string {
	return fmt.Sprintf(`FROM %s
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl wget ca-certificates openssh-client \
    git ripgrep fd-find tree less \
    build-essential pkg-config \
    jq unzip zip tar gzip \
    sudo locales \
  && sed -i 's/# en_US.UTF-8/en_US.UTF-8/' /etc/locale.gen \
  && locale-gen \
  && rm -rf /var/lib/apt/lists/*
`, base)
}
