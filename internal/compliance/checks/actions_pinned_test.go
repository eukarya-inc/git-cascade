package checks

import (
	"testing"
)

func TestSHARefPattern(t *testing.T) {
	tests := []struct {
		ref  string
		want bool
	}{
		{"abc123def456abc123def456abc123def456abc1", true},  // 40-char hex
		{"v2", false},
		{"main", false},
		{"v1.2.3", false},
		{"abc123", false}, // too short
	}

	for _, tt := range tests {
		got := shaRef.MatchString(tt.ref)
		if got != tt.want {
			t.Errorf("shaRef.MatchString(%q) = %v, want %v", tt.ref, got, tt.want)
		}
	}
}

func TestUsesPatternExtraction(t *testing.T) {
	workflow := `
jobs:
  build:
    steps:
      - uses: actions/checkout@abc123def456abc123def456abc123def456abc1
      - uses: actions/setup-node@v3
      - uses: docker://alpine:3.18
      - uses: ./.github/actions/local
      - uses: 'owner/action@abc123def456abc123def456abc123def456abc1'
`

	matches := usesPattern.FindAllStringSubmatch(workflow, -1)
	// docker:// and local actions don't have @, so only 3 matches
	if len(matches) != 3 {
		t.Fatalf("expected 3 uses matches, got %d: %v", len(matches), matches)
	}

	// First: pinned checkout
	if matches[0][1] != "actions/checkout" {
		t.Errorf("expected actions/checkout, got %s", matches[0][1])
	}

	// Second: unpinned setup-node
	if matches[1][1] != "actions/setup-node" {
		t.Errorf("expected actions/setup-node, got %s", matches[1][1])
	}
	if matches[1][2] != "v3" {
		t.Errorf("expected ref v3, got %s", matches[1][2])
	}

	// Third: quoted pinned action
	if matches[2][1] != "owner/action" {
		t.Errorf("expected owner/action, got %s", matches[2][1])
	}
}

func TestFindUnpinnedActions_ActionsLock(t *testing.T) {
	lock, err := ParseActionsLock([]byte(`version: 'v0.0.3'
workflows:
    '.github/workflows/ci.yaml':
        - 'actions/checkout@v7.0.1'
        - 'actions/cache@v6.1.0'
dependencies:
    'actions/checkout@v7.0.1':
        ref: 'v7.0.1'
        commit: 'sha1-3d3c42e5aac5ba805825da76410c181273ba90b1'
`))
	if err != nil {
		t.Fatal(err)
	}
	workflow := `
steps:
  - uses: actions/checkout@v7.0.1
  - uses: actions/cache@v6.1.0
  - uses: actions/setup-go@v5
`
	// checkout: locked. cache: listed but no dependency commit. setup-go: not in lock.
	got := FindUnpinnedActions(workflow, ".github/workflows/ci.yaml", lock)
	if len(got) != 2 || got[0].Action != "actions/cache" || got[1].Action != "actions/setup-go" {
		t.Errorf("unexpected violations: %+v", got)
	}

	// Lock entries are per workflow: same reference in another file is unpinned.
	if got := FindUnpinnedActions(workflow, ".github/workflows/other.yaml", lock); len(got) != 3 {
		t.Errorf("expected 3 violations for workflow absent from lock, got %+v", got)
	}

	// No lock behaves as before.
	if got := FindUnpinnedActions(workflow, ".github/workflows/ci.yaml", nil); len(got) != 3 {
		t.Errorf("expected 3 violations without lock, got %+v", got)
	}
}
