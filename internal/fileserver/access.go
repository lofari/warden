package fileserver

import (
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// DefaultDenyPatterns is the built-in deny-list, active unless overridden.
var DefaultDenyPatterns = []string{
	"**/.env",
	"**/.env.*",
	"**/*.pem",
	"**/*.key",
	"**/*.p12",
	"**/*.pfx",
	"**/.npmrc",
	"**/.pypirc",
	".git/credentials",
	".git/config",
	"**/.ssh/*",
	"**/.aws/*",
	"**/.gnupg/*",
	"**/.docker/config.json",
}

// AccessControl manages deny-list and read-only override patterns.
type AccessControl struct {
	denyPatterns     []string
	readOnlyPatterns []string
}

// NewAccessControl creates an AccessControl.
//   - denyExtra: additional patterns added to built-in defaults
//   - denyOverride: if non-nil, replaces built-in defaults entirely
//   - readOnly: paths that are read-only within an rw mount
func NewAccessControl(denyExtra, denyOverride, readOnly []string) *AccessControl {
	var deny []string
	if denyOverride != nil {
		deny = denyOverride
	} else {
		deny = append(deny, DefaultDenyPatterns...)
		deny = append(deny, denyExtra...)
	}
	return &AccessControl{
		denyPatterns:     deny,
		readOnlyPatterns: readOnly,
	}
}

// IsDenied returns true if the relative path matches any deny pattern.
func (ac *AccessControl) IsDenied(relPath string) bool {
	if ac == nil {
		return false
	}
	relPath = filepath.ToSlash(relPath)
	for _, pattern := range ac.denyPatterns {
		pattern = filepath.ToSlash(pattern)
		if matched, _ := doublestar.Match(pattern, relPath); matched {
			return true
		}
		// Directory pattern: "dir/" matches everything under dir
		if strings.HasSuffix(pattern, "/") && strings.HasPrefix(relPath, pattern) {
			return true
		}
	}
	return false
}

// IsReadOnly returns true if the relative path falls under a read-only override.
func (ac *AccessControl) IsReadOnly(relPath string) bool {
	if ac == nil {
		return false
	}
	relPath = filepath.ToSlash(relPath)
	for _, pattern := range ac.readOnlyPatterns {
		pattern = filepath.ToSlash(pattern)
		// Exact match
		if matched, _ := doublestar.Match(pattern, relPath); matched {
			return true
		}
		// Prefix match: if pattern is "X", then "X/anything" is also read-only
		if strings.HasPrefix(relPath, pattern+"/") {
			return true
		}
	}
	return false
}

// NoAccessControl returns a nil AccessControl (no restrictions).
func NoAccessControl() *AccessControl {
	return nil
}
