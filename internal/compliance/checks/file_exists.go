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

// fileExistsChecker checks that one or more files exist in the repository.
type fileExistsChecker struct {
	id    string
	files []string // files to look for (any match = pass)
}

func (c *fileExistsChecker) ID() string { return c.id }

func (c *fileExistsChecker) Check(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule) (*compliance.Result, error) {
	ref := repo.DefaultBranch

	for _, path := range c.files {
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
				Message:  fmt.Sprintf("found %s", path),
			}, nil
		}
	}

	return &compliance.Result{
		RuleID:   rule.ID,
		RuleName: rule.Name,
		Repo:     repo.FullName,
		Status:   compliance.StatusFail,
		Severity: rule.Severity,
		Message:  fmt.Sprintf("none of [%s] found", strings.Join(c.files, ", ")),
	}, nil
}

func init() {
	compliance.Register(&fileExistsChecker{
		id:    "readme-exists",
		files: []string{"README.md", "README", "README.rst", "readme.md"},
	})
	compliance.Register(&fileExistsChecker{
		id:    "license-exists",
		files: []string{"LICENSE", "LICENSE.md", "LICENSE.txt", "LICENCE", "LICENCE.md"},
	})
}
