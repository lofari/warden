package fileserver

import "testing"

func TestBuiltInDenyDefaults(t *testing.T) {
	ac := NewAccessControl(nil, nil, nil)
	tests := []struct {
		path string
		want bool
	}{
		{".env", true},
		{".env.local", true},
		{".env.production", true},
		{"src/.env", true}, // **/.env matches at any depth
		{"secrets.pem", true},
		{"cert.key", true},
		{"cert.p12", true},
		{".git/credentials", true},
		{".git/config", true},
		{".ssh/id_rsa", true},
		{"home/.ssh/id_rsa", true},
		{".aws/credentials", true},
		{".npmrc", true},
		{".pypirc", true},
		{".docker/config.json", true},
		{"src/main.go", false},
		{"README.md", false},
		{".git/HEAD", false},
	}
	for _, tt := range tests {
		if got := ac.IsDenied(tt.path); got != tt.want {
			t.Errorf("IsDenied(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestDenyExtra(t *testing.T) {
	ac := NewAccessControl([]string{"secrets/", "*.secret"}, nil, nil)
	if !ac.IsDenied(".env") {
		t.Error("built-in .env should still be denied")
	}
	if !ac.IsDenied("secrets/db.yml") {
		t.Error("secrets/ should be denied via deny_extra")
	}
	if !ac.IsDenied("app.secret") {
		t.Error("*.secret should be denied via deny_extra")
	}
	if ac.IsDenied("src/main.go") {
		t.Error("src/main.go should not be denied")
	}
}

func TestDenyOverride(t *testing.T) {
	ac := NewAccessControl(nil, []string{".env"}, nil)
	if !ac.IsDenied(".env") {
		t.Error(".env should be denied")
	}
	// Built-in defaults are replaced — .pem should NOT be denied
	if ac.IsDenied("cert.pem") {
		t.Error("cert.pem should not be denied when override replaces defaults")
	}
}

func TestReadOnlyPatterns(t *testing.T) {
	ac := NewAccessControl(nil, nil, []string{".git/hooks", ".github/workflows", "Makefile"})
	tests := []struct {
		path string
		want bool
	}{
		{".git/hooks/pre-commit", true},
		{".git/hooks", true},
		{".github/workflows/ci.yml", true},
		{"Makefile", true},
		{"src/main.go", false},
		{".git/HEAD", false},
	}
	for _, tt := range tests {
		if got := ac.IsReadOnly(tt.path); got != tt.want {
			t.Errorf("IsReadOnly(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
