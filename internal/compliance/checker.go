package compliance

import (
	"context"

	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/google/go-github/v84/github"
)

// Result represents the outcome of a single compliance check on a repository.
type Result struct {
	RuleID   string          `json:"rule_id"`
	RuleName string          `json:"rule_name"`
	Repo     string          `json:"repo"`
	Status   Status          `json:"status"`
	Severity config.Severity `json:"severity"`
	Message  string          `json:"message"`
}

// Status is the result of a compliance check.
type Status string

const (
	StatusPass Status = "pass"
	StatusFail Status = "fail"
	StatusSkip Status = "skip"
)

// Checker evaluates a single compliance rule against a repository.
type Checker interface {
	// ID returns the rule ID this checker handles.
	ID() string
	// Check runs the compliance check and returns results.
	Check(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule) (*Result, error)
}

// registry holds all registered checkers keyed by rule ID.
var registry = map[string]Checker{}

// Register adds a checker to the global registry.
func Register(c Checker) {
	registry[c.ID()] = c
}

// GetChecker returns the checker for a given rule ID, or nil if not found.
func GetChecker(id string) Checker {
	return registry[id]
}

// ListCheckers returns all registered checker IDs.
func ListCheckers() []string {
	ids := make([]string, 0, len(registry))
	for id := range registry {
		ids = append(ids, id)
	}
	return ids
}
