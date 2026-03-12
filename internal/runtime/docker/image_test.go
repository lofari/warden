package docker

import "testing"

func TestImageTagNoTools(t *testing.T) {
	tag := ImageTag("ubuntu:24.04", nil)
	want := "warden:base-ubuntu-24.04"
	if tag != want {
		t.Errorf("ImageTag = %q, want %q", tag, want)
	}
}

func TestImageTagSorted(t *testing.T) {
	tag := ImageTag("ubuntu:24.04", []string{"python", "node"})
	want := "warden:ubuntu-24.04_node_python"
	if tag != want {
		t.Errorf("ImageTag = %q, want %q", tag, want)
	}
}

func TestBaseImageTag(t *testing.T) {
	tag := BaseImageTag("ubuntu:24.04")
	want := "warden:base-ubuntu-24.04"
	if tag != want {
		t.Errorf("BaseImageTag = %q, want %q", tag, want)
	}
}
