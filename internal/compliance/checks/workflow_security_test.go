package checks

import (
	"testing"
)

// jobNames extracts job names from FindJobsMissingHardenRunner's result, for
// tests written against the pre-refactor []string return shape.
func jobNames(missing []MissingHardenRunnerJob) []string {
	if missing == nil {
		return nil
	}
	names := make([]string, len(missing))
	for i, m := range missing {
		names[i] = m.Job
	}
	return names
}

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

// — secretsInheritLines ———————————————————————————————————————————————————————

func TestSecretsInheritLines(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []int
	}{
		{
			name: "secrets inherit in reusable workflow call",
			content: `
jobs:
  call:
    uses: org/repo/.github/workflows/reusable.yml@main
    secrets: inherit
`,
			want: []int{5},
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
			want: nil,
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
			want: nil,
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
			want: nil,
		},
		{
			name: "indented secrets inherit",
			content: `
jobs:
  call:
    uses: org/repo/.github/workflows/reusable.yml@main
      secrets: inherit
`,
			want: []int{5},
		},
		{
			name: "multiple secrets inherit occurrences",
			content: `
jobs:
  call1:
    uses: org/repo/.github/workflows/a.yml@main
    secrets: inherit
  call2:
    uses: org/repo/.github/workflows/b.yml@main
    secrets: inherit
`,
			want: []int{5, 8},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := secretsInheritLines(tt.content)
			if len(got) != len(tt.want) {
				t.Errorf("secretsInheritLines() = %v, want %v\ncontent:\n%s", got, tt.want, tt.content)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("secretsInheritLines()[%d] = %d, want %d\ncontent:\n%s", i, got[i], tt.want[i], tt.content)
				}
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
			got := jobNames(FindJobsMissingHardenRunner(tt.content))
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

func TestIndentOf(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"no indent", 0},
		{"  two spaces", 2},
		{"    four spaces", 4},
		{"      six spaces", 6},
		{"", 0},
		{"  ", 2},
	}
	for _, tt := range tests {
		got := indentOf(tt.input)
		if got != tt.want {
			t.Errorf("indentOf(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestJobsMissingHardenRunner_CommentedLines(t *testing.T) {
	// Comment lines must be skipped and not cause false positives.
	content := `
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: step-security/harden-runner@abc123
      # - uses: some/other-action@old
      - uses: actions/checkout@abc123
`
	missing := jobNames(FindJobsMissingHardenRunner(content))
	if len(missing) != 0 {
		t.Errorf("expected no missing jobs, got %v", missing)
	}
}

func TestJobsMissingHardenRunner_TopLevelKeyEndsJobs(t *testing.T) {
	// A top-level key after jobs: should terminate job parsing.
	content := `
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@abc123
permissions:
  contents: read
`
	missing := jobNames(FindJobsMissingHardenRunner(content))
	if len(missing) != 1 || missing[0] != "build" {
		t.Errorf("expected [build] missing, got %v", missing)
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
