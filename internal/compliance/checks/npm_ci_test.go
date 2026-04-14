package checks

import (
	"testing"
)

func TestHasNpmInstallViolation(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"plain npm install", "run: npm install", true},
		{"npm install with newline", "run: npm install\nnext line", true},
		{"npm ci is fine", "run: npm ci", false},
		{"global install is fine", "run: npm install -g typescript", false},
		{"mixed global and local", "run: npm install -g typescript\nrun: npm install", true},
		{"only global", "npm install -g eslint\nnpm install -g prettier", false},
		{"npm ci with flags", "run: npm ci --ignore-scripts", false},
		{"no npm at all", "run: go build ./...", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasNpmInstallViolation(tt.input)
			if got != tt.want {
				t.Errorf("hasNpmInstallViolation(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
