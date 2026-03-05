package container

import (
	"fmt"
	"strings"
)

// BaseImageTag returns the tag for the warden base image derived from a given base.
func BaseImageTag(base string) string {
	safe := strings.ReplaceAll(base, ":", "-")
	safe = strings.ReplaceAll(safe, "/", "-")
	return "warden:base-" + safe
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
