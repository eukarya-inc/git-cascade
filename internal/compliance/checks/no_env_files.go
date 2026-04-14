package checks

import (
	"context"
	"fmt"
	"strings"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/google/go-github/v84/github"
)

// envFileCandidates are the .env file names that must not be committed.
// .env.example is intentionally excluded as it is safe to commit.
var envFileCandidates = []string{
	".env",
	".env.local",
	".env.production",
	".env.development",
	".env.staging",
	".env.test",
}

type noEnvFilesChecker struct{}

func (c *noEnvFilesChecker) ID() string { return "no-env-files" }

func (c *noEnvFilesChecker) Check(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule) (*compliance.Result, error) {
	ref := repo.DefaultBranch

	// Scan the root directory listing to detect any .env* files.
	dirContent, err := gh.ListDirectoryContents(ctx, client, repo.Owner, repo.Name, "", ref)
	if err != nil {
		return nil, err
	}

	var found []string
	for _, entry := range dirContent {
		name := entry.GetName()
		if isEnvFile(name) {
			found = append(found, name)
		}
	}

	if len(found) > 0 {
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusFail,
			Severity: rule.Severity,
			Message:  fmt.Sprintf("env file(s) committed: %s", strings.Join(found, ", ")),
		}, nil
	}

	return &compliance.Result{
		RuleID:   rule.ID,
		RuleName: rule.Name,
		Repo:     repo.FullName,
		Status:   compliance.StatusPass,
		Severity: rule.Severity,
		Message:  "no .env files committed",
	}, nil
}

// isEnvFile returns true if name matches a disallowed .env file.
// .env.example is always allowed.
func isEnvFile(name string) bool {
	if name == ".env.example" {
		return false
	}
	for _, candidate := range envFileCandidates {
		if name == candidate {
			return true
		}
	}
	// Also catch any other .env.* variants not in the explicit list.
	if strings.HasPrefix(name, ".env.") || name == ".env" {
		return true
	}
	return false
}

func init() {
	compliance.Register(&noEnvFilesChecker{})
}
