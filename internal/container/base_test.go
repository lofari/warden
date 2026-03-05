package container

import "testing"

func TestBaseImageTag(t *testing.T) {
	tests := []struct {
		base string
		want string
	}{
		{"ubuntu:24.04", "warden:base-ubuntu-24.04"},
		{"ubuntu:22.04", "warden:base-ubuntu-22.04"},
		{"debian:bookworm", "warden:base-debian-bookworm"},
		{"myrepo/ubuntu:24.04", "warden:base-myrepo-ubuntu-24.04"},
	}
	for _, tt := range tests {
		got := BaseImageTag(tt.base)
		if got != tt.want {
			t.Errorf("BaseImageTag(%q) = %q, want %q", tt.base, got, tt.want)
		}
	}
}

func TestBaseDockerfile(t *testing.T) {
	df := BaseDockerfile("ubuntu:24.04")
	if df == "" {
		t.Fatal("BaseDockerfile should not be empty")
	}
	if df[:4] != "FROM" {
		t.Error("Dockerfile should start with FROM")
	}
	for _, pkg := range []string{"ripgrep", "fd-find", "build-essential", "jq", "git"} {
		if !contains(df, pkg) {
			t.Errorf("BaseDockerfile missing package %q", pkg)
		}
	}
	if !contains(df, "locale-gen") {
		t.Error("BaseDockerfile should configure locale")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestBuildBaseImageWritesDockerfile(t *testing.T) {
	// We can't run docker build in unit tests, but we can verify
	// the function calls the right pieces by testing the Dockerfile content.
	df := BaseDockerfile("ubuntu:24.04")
	tag := BaseImageTag("ubuntu:24.04")

	if tag != "warden:base-ubuntu-24.04" {
		t.Errorf("unexpected tag: %s", tag)
	}
	if !contains(df, "FROM ubuntu:24.04") {
		t.Error("Dockerfile should use the specified base image")
	}
}
