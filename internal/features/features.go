package features

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed all:scripts
var scriptsFS embed.FS

// GetFeatureScript returns the contents of a feature install script.
func GetFeatureScript(name string) ([]byte, error) {
	data, err := scriptsFS.ReadFile("scripts/" + name + ".sh")
	if err != nil {
		return nil, fmt.Errorf("unknown feature: %q", name)
	}
	return data, nil
}

// AvailableFeatures returns sorted list of built-in feature names.
func AvailableFeatures() []string {
	entries, _ := scriptsFS.ReadDir("scripts")
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sh") {
			names = append(names, strings.TrimSuffix(e.Name(), ".sh"))
		}
	}
	sort.Strings(names)
	return names
}
