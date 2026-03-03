package container

import (
	"strings"
	"testing"
)

func TestContainerName(t *testing.T) {
	name := ContainerName()
	if !strings.HasPrefix(name, "warden-") {
		t.Errorf("container name %q should start with warden-", name)
	}
}

func TestJoinArgsQuotesSpaces(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"simple", []string{"echo", "hello"}, "echo hello"},
		{"spaces", []string{"echo", "hello world"}, "echo 'hello world'"},
		{"single_quote", []string{"echo", "it's"}, "echo 'it'\\''s'"},
		{"double_quote", []string{"echo", `say "hi"`}, `echo 'say "hi"'`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := joinArgs(tt.args)
			if got != tt.want {
				t.Errorf("joinArgs(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}
