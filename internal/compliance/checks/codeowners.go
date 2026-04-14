package checks

import (
	"context"
	"fmt"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/google/go-github/v84/github"
)

type codeownersChecker struct{}

func (c *codeownersChecker) ID() string { return "codeowners-exists" }

func (c *codeownersChecker) Check(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule) (*compliance.Result, error) {
	ref := repo.DefaultBranch
	paths := []string{
		".github/CODEOWNERS",
		"CODEOWNERS",
		"docs/CODEOWNERS",
	}

	for _, path := range paths {
		content, err := gh.FetchFileContent(ctx, client, repo.Owner, repo.Name, path, ref)
		if err != nil {
			return nil, err
		}
		if content != nil {
			return &compliance.Result{
				RuleID:   rule.ID,
				RuleName: rule.Name,
				Repo:     repo.FullName,
				Status:   compliance.StatusPass,
				Severity: rule.Severity,
				Message:  fmt.Sprintf("CODEOWNERS found at %s", path),
			}, nil
		}
	}

	return &compliance.Result{
		RuleID:   rule.ID,
		RuleName: rule.Name,
		Repo:     repo.FullName,
		Status:   compliance.StatusFail,
		Severity: rule.Severity,
		Message:  "CODEOWNERS not found in .github/, root, or docs/",
	}, nil
}

func init() {
	compliance.Register(&codeownersChecker{})
}
