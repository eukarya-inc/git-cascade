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

type externalCollaboratorsChecker struct{}

func (c *externalCollaboratorsChecker) ID() string { return "external-collaborators" }

func (c *externalCollaboratorsChecker) Check(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule) (*compliance.Result, error) {
	// List outside collaborators with direct access
	opts := &github.ListCollaboratorsOptions{
		Affiliation: "outside",
		ListOptions: github.ListOptions{PerPage: 100},
	}

	var adminCollabs []string
	for {
		collabs, resp, err := client.Repositories.ListCollaborators(ctx, repo.Owner, repo.Name, opts)
		if err != nil {
			if resp != nil && resp.StatusCode == 403 {
				return &compliance.Result{
					RuleID:   rule.ID,
					RuleName: rule.Name,
					Repo:     repo.FullName,
					Status:   compliance.StatusSkip,
					Severity: rule.Severity,
					Message:  "insufficient permissions to list collaborators",
				}, nil
			}
			return nil, fmt.Errorf("listing collaborators for %s: %w", repo.FullName, err)
		}

		for _, collab := range collabs {
			if perms := collab.GetPermissions(); perms != nil && perms.GetAdmin() {
				adminCollabs = append(adminCollabs, collab.GetLogin())
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	if len(adminCollabs) > 0 {
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusFail,
			Severity: rule.Severity,
			Message:  fmt.Sprintf("external collaborators with admin access: %s", strings.Join(adminCollabs, ", ")),
		}, nil
	}

	return &compliance.Result{
		RuleID:   rule.ID,
		RuleName: rule.Name,
		Repo:     repo.FullName,
		Status:   compliance.StatusPass,
		Severity: rule.Severity,
		Message:  "no external collaborators with admin access",
	}, nil
}

func init() {
	compliance.Register(&externalCollaboratorsChecker{})
}
