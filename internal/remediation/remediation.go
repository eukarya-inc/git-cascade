// Package remediation opens pull requests that automatically fix certain
// compliance violations. It mirrors the internal/compliance Checker registry
// pattern: each rule ID that supports auto-remediation registers a
// Remediator, and the engine dispatches failing results to the matching one.
package remediation

import (
	"context"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/google/go-github/v84/github"
)

// FileChange is a single file to create or overwrite in a remediation commit.
type FileChange struct {
	Path    string
	Content []byte
}

// Fix describes the file changes and pull request metadata a Remediator
// wants applied for one failing result. A nil Fix (or one with no Files)
// means the remediator found nothing it can safely fix.
type Fix struct {
	Files         []FileChange
	CommitMessage string
	PRTitle       string
	PRBody        string
}

// Remediator produces a Fix for a failing compliance result of the rule it handles.
type Remediator interface {
	// ID returns the rule ID this remediator handles. Must match a registered compliance.Checker's ID.
	ID() string
	// Remediate inspects the repository and returns the file changes needed
	// to fix the given failing result, or (nil, nil) if there is nothing it
	// can safely fix (e.g. the ref can't be resolved).
	Remediate(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule, result compliance.Result) (*Fix, error)
}

// registry holds all registered remediators keyed by rule ID.
var registry = map[string]Remediator{}

// Register adds a remediator to the global registry.
func Register(r Remediator) {
	registry[r.ID()] = r
}

// Get returns the remediator for a rule ID, or nil if none is registered.
func Get(id string) Remediator {
	return registry[id]
}
