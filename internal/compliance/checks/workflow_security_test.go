package checks

import (
	"testing"
)

// — hasPullRequestTarget ——————————————————————————————————————————————————————

func TestHasPullRequestTarget(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name: "bare trigger as map key",
			content: `
on:
  pull_request_target:
    types: [opened]
`,
			want: true,
		},
		{
			name: "inline flow sequence not detected (known limitation)",
			content: `
on: [push, pull_request_target]
`,
			// Inline flow sequences are not parsed; authors should use block style.
			want: false,
		},
		{
			name: "trigger as standalone list item",
			content: `
on:
  - push
  - pull_request_target
`,
			want: true,
		},
		{
			name: "safe workflow with pull_request only",
			content: `
on:
  pull_request:
    branches: [main]
`,
			want: false,
		},
		{
			name: "no triggers at all",
			content: `
on:
  push:
    branches: [main]
`,
			want: false,
		},
		{
			name: "pull_request_target in a comment should not match",
			content: `
# using pull_request_target is discouraged
on:
  push:
`,
			// comments are plain lines; "# using pull_request_target" doesn't match
			// our trimmed equality / HasPrefix check — this is correct behaviour
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasPullRequestTarget(tt.content)
			if got != tt.want {
				t.Errorf("hasPullRequestTarget() = %v, want %v\ncontent:\n%s", got, tt.want, tt.content)
			}
		})
	}
}

// — hasSecretsInherit —————————————————————————————————————————————————————————

func TestHasSecretsInherit(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name: "secrets inherit in reusable workflow call",
			content: `
jobs:
  call:
    uses: org/repo/.github/workflows/reusable.yml@main
    secrets: inherit
`,
			want: true,
		},
		{
			name: "explicit secrets mapping is fine",
			content: `
jobs:
  call:
    uses: org/repo/.github/workflows/reusable.yml@main
    secrets:
      MY_TOKEN: ${{ secrets.MY_TOKEN }}
`,
			want: false,
		},
		{
			name: "no secrets block at all",
			content: `
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@abc123def456abc123def456abc123def456abc1
`,
			want: false,
		},
		{
			name: "secrets inherit in comment should not match",
			content: `
# do not use secrets: inherit
jobs:
  build:
    runs-on: ubuntu-latest
`,
			// "# do not use secrets: inherit" — trimmed is "# do not use secrets: inherit"
			// which does not equal "secrets: inherit"
			want: false,
		},
		{
			name: "indented secrets inherit",
			content: `
jobs:
  call:
    uses: org/repo/.github/workflows/reusable.yml@main
      secrets: inherit
`,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasSecretsInherit(tt.content)
			if got != tt.want {
				t.Errorf("hasSecretsInherit() = %v, want %v\ncontent:\n%s", got, tt.want, tt.content)
			}
		})
	}
}
