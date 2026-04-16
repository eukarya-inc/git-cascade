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

// — jobsMissingHardenRunner ———————————————————————————————————————————————————

func TestJobsMissingHardenRunner(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string // nil means no violations
	}{
		{
			name: "single job with harden-runner as first step",
			content: `
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - name: Harden Runner
        uses: step-security/harden-runner@fe104658747b27e96e4f7e80cd0a94068e53901d # v2.16.1
        with:
          egress-policy: audit
      - uses: actions/checkout@abc123
`,
			want: nil,
		},
		{
			name: "single job with harden-runner uses: on same line as dash",
			content: `
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: step-security/harden-runner@fe104658747b27e96e4f7e80cd0a94068e53901d
      - uses: actions/checkout@abc123
`,
			want: nil,
		},
		{
			name: "single job missing harden-runner (first step is checkout)",
			content: `
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@abc123
      - run: go build ./...
`,
			want: []string{"build"},
		},
		{
			name: "harden-runner present but not first step",
			content: `
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@abc123
      - uses: step-security/harden-runner@fe104658747b27e96e4f7e80cd0a94068e53901d
`,
			want: []string{"build"},
		},
		{
			name: "multiple jobs, all compliant",
			content: `
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: step-security/harden-runner@abc123
      - uses: actions/checkout@abc123
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: step-security/harden-runner@def456
      - run: go test ./...
`,
			want: nil,
		},
		{
			name: "multiple jobs, one missing harden-runner",
			content: `
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: step-security/harden-runner@abc123
      - uses: actions/checkout@abc123
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@abc123
      - run: ./deploy.sh
`,
			want: []string{"deploy"},
		},
		{
			name: "multiple jobs, all missing harden-runner",
			content: `
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@abc123
  test:
    runs-on: ubuntu-latest
    steps:
      - run: go test ./...
`,
			want: []string{"build", "test"},
		},
		{
			name: "job with no steps block is ignored",
			content: `
on: [workflow_call]
jobs:
  call:
    uses: org/repo/.github/workflows/reusable.yml@main
`,
			want: nil,
		},
		{
			name: "no jobs block at all",
			content: `
on: [push]
`,
			want: nil,
		},
		{
			name: "harden-runner with SHA and inline version comment",
			content: `
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - name: Harden Runner
        uses: step-security/harden-runner@fe104658747b27e96e4f7e80cd0a94068e53901d # v2.16.1
        with:
          egress-policy: block
`,
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jobsMissingHardenRunner(tt.content)
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("jobsMissingHardenRunner() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("jobsMissingHardenRunner()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestIsHardenRunner(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"step-security/harden-runner@fe104658747b27e96e4f7e80cd0a94068e53901d # v2.16.1", true},
		{"step-security/harden-runner@v2", true},
		{"step-security/harden-runner@abc123", true},
		{"actions/checkout@abc123", false},
		{"step-security/other-action@abc123", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got := isHardenRunner(tt.value)
			if got != tt.want {
				t.Errorf("isHardenRunner(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}
