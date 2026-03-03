package features

import (
	"testing"
)

func TestGetFeatureScript(t *testing.T) {
	script, err := GetFeatureScript("node")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(script) == 0 {
		t.Fatal("script should not be empty")
	}
}

func TestGetFeatureScriptUnknown(t *testing.T) {
	_, err := GetFeatureScript("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown feature")
	}
}

func TestAvailableFeatures(t *testing.T) {
	features := AvailableFeatures()
	expected := []string{"go", "java", "node", "python", "rust"}
	if len(features) != len(expected) {
		t.Fatalf("got %d features, want %d", len(features), len(expected))
	}
	for i, f := range features {
		if f != expected[i] {
			t.Errorf("feature[%d] = %q, want %q", i, f, expected[i])
		}
	}
}
