package checks

import (
	"testing"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
)

// TestCheckerIDsRegistered verifies that all expected checkers are registered
// with the correct IDs via their init() functions.
func TestCheckerIDsRegistered(t *testing.T) {
	want := []string{
		"codeowners-exists",
		"readme-exists",
		"license-exists",
		"lockfile-required",
		"npm-ci-required",
		"no-env-files",
		"renovate-config",
		"actions-pinned",
		"dockerfile-digest",
		"ai-config-safety",
		"no-pull-request-target",
		"no-secrets-inherit",
		"harden-runner-required",
		"branch-protection",
		"external-collaborators",
	}

	for _, id := range want {
		t.Run(id, func(t *testing.T) {
			c := compliance.GetChecker(id)
			if c == nil {
				t.Errorf("checker %q not registered", id)
				return
			}
			if c.ID() != id {
				t.Errorf("checker ID() = %q, want %q", c.ID(), id)
			}
		})
	}
}
