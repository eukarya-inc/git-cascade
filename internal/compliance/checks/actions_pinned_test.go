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
