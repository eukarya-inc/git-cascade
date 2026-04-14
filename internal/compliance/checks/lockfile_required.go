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

// manifestToLockfile maps manifest files to their expected lockfiles.
var manifestToLockfile = map[string][]string{
	"package.json": {"package-lock.json", "yarn.lock", "pnpm-lock.yaml", "bun.lockb"},
	"go.mod":       {"go.sum"},
	"Cargo.toml":   {"Cargo.lock"},
	"pyproject.toml": {"uv.lock", "poetry.lock", "requirements.txt"},
}

type lockfileRequiredChecker struct{}

func (c *lockfileRequiredChecker) ID() string { return "lockfile-required" }

func (c *lockfileRequiredChecker) Check(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule) (*compliance.Result, error) {
	ref := repo.DefaultBranch

	var missing []string

	for manifest, lockfiles := range manifestToLockfile {
		// Check if the manifest file exists
		manifestContent, err := gh.FetchFileContent(ctx, client, repo.Owner, repo.Name, manifest, ref)
		if err != nil {
			return nil, err
		}
		if manifestContent == nil {
			continue // This ecosystem is not used in this repo
		}

		// Manifest exists — check that at least one lockfile is present
		found := false
		for _, lf := range lockfiles {
			content, err := gh.FetchFileContent(ctx, client, repo.Owner, repo.Name, lf, ref)
			if err != nil {
				return nil, err
			}
			if content != nil {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, fmt.Sprintf("%s (expected one of: %s)", manifest, strings.Join(lockfiles, ", ")))
		}
	}

	if len(missing) > 0 {
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusFail,
			Severity: rule.Severity,
			Message:  fmt.Sprintf("missing lockfile for: %s", strings.Join(missing, "; ")),
		}, nil
	}

	return &compliance.Result{
		RuleID:   rule.ID,
		RuleName: rule.Name,
		Repo:     repo.FullName,
		Status:   compliance.StatusPass,
		Severity: rule.Severity,
		Message:  "all manifests have corresponding lockfiles",
	}, nil
}

func init() {
	compliance.Register(&lockfileRequiredChecker{})
}
