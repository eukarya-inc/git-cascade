package checks

import (
	"testing"
)

func TestIsEnvFile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		// Must be blocked
		{".env", true},
		{".env.local", true},
		{".env.production", true},
		{".env.development", true},
		{".env.staging", true},
		{".env.test", true},
		{".env.custom", true}, // any unknown .env.* variant
		// Must be allowed
		{".env.example", false},
		{"README.md", false},
		{".gitignore", false},
		{"env.sh", false},
	}

	for _, tt := range tests {
		got := isEnvFile(tt.name)
		if got != tt.want {
			t.Errorf("isEnvFile(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
