package checks

import (
	"testing"
)

func TestInstallViolation(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		// npm
		{"npm install triggers violation", "run: npm install", "npm install"},
		{"npm install with newline", "run: npm install\nnext line", "npm install"},
		{"npm ci is fine", "run: npm ci", ""},
		{"npm global install is fine", "run: npm install -g typescript", ""},
		{"mixed global and local npm", "run: npm install -g typescript\nrun: npm install", "npm install"},
		{"only global npm", "npm install -g eslint\nnpm install -g prettier", ""},
		{"npm ci with flags", "run: npm ci --ignore-scripts", ""},

		// pnpm
		{"pnpm install triggers violation", "run: pnpm install", "pnpm install without --frozen-lockfile"},
		{"pnpm install with frozen-lockfile is fine", "run: pnpm install --frozen-lockfile", ""},
		{"pnpm install frozen-lockfile inline", "pnpm install --frozen-lockfile --prefer-offline", ""},
		{"pnpm add is fine", "run: pnpm add lodash", ""},

		// yarn
		{"bare yarn triggers violation", "run: yarn", "yarn install without --frozen-lockfile/--immutable"},
		{"yarn install triggers violation", "run: yarn install", "yarn install without --frozen-lockfile/--immutable"},
		{"yarn install with frozen-lockfile is fine", "run: yarn install --frozen-lockfile", ""},
		{"yarn install with immutable is fine", "run: yarn install --immutable", ""},
		{"yarn with immutable is fine", "run: yarn --immutable", ""},
		{"yarn add is fine", "run: yarn add lodash", ""},
		{"yarn run is fine", "run: yarn run build", ""},
		{"yarn build is fine", "run: yarn build", ""},

		// other
		{"no npm at all", "run: go build ./...", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := installViolation(tt.input)
			if got != tt.want {
				t.Errorf("installViolation(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestHasNpmInstallViolation(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"plain npm install", "run: npm install", true},
		{"npm ci is fine", "run: npm ci", false},
		{"global install is fine", "run: npm install -g typescript", false},
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

func TestHasPnpmInstallViolation(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"pnpm install triggers", "run: pnpm install", true},
		{"pnpm install with frozen-lockfile is fine", "run: pnpm install --frozen-lockfile", false},
		{"pnpm add is fine", "run: pnpm add react", false},
		{"no pnpm", "run: npm ci", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasPnpmInstallViolation(tt.input)
			if got != tt.want {
				t.Errorf("hasPnpmInstallViolation(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestHasYarnInstallViolation(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"bare yarn triggers", "run: yarn", true},
		{"yarn install triggers", "run: yarn install", true},
		{"yarn install frozen-lockfile is fine", "run: yarn install --frozen-lockfile", false},
		{"yarn install immutable is fine", "run: yarn install --immutable", false},
		{"yarn immutable is fine", "run: yarn --immutable", false},
		{"yarn add is fine", "run: yarn add react", false},
		{"yarn build is fine", "run: yarn build", false},
		{"yarn run is fine", "run: yarn run test", false},
		{"no yarn", "run: npm ci", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasYarnInstallViolation(tt.input)
			if got != tt.want {
				t.Errorf("hasYarnInstallViolation(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
