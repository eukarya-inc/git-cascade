package checks

import (
	"context"
	"fmt"
	"strconv"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/google/go-github/v84/github"
)

type branchProtectionChecker struct{}

func (c *branchProtectionChecker) ID() string { return "branch-protection" }

func (c *branchProtectionChecker) Check(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule) (*compliance.Result, error) {
	branch := repo.DefaultBranch
	if branch == "" {
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusSkip,
			Severity: rule.Severity,
			Message:  "no default branch set",
		}, nil
	}

	protection, resp, err := client.Repositories.GetBranchProtection(ctx, repo.Owner, repo.Name, branch)
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return &compliance.Result{
				RuleID:   rule.ID,
				RuleName: rule.Name,
				Repo:     repo.FullName,
				Status:   compliance.StatusFail,
				Severity: rule.Severity,
				Message:  fmt.Sprintf("branch protection not enabled on %s", branch),
			}, nil
		}
		return nil, fmt.Errorf("fetching branch protection for %s/%s:%s: %w", repo.Owner, repo.Name, branch, err)
	}

	// Check optional params
	if rule.Params["require_reviews"] == "true" {
		if protection.RequiredPullRequestReviews == nil {
			return &compliance.Result{
				RuleID:   rule.ID,
				RuleName: rule.Name,
				Repo:     repo.FullName,
				Status:   compliance.StatusFail,
				Severity: rule.Severity,
				Message:  "pull request reviews not required",
			}, nil
		}
		if minStr, ok := rule.Params["required_reviewers"]; ok {
			min, _ := strconv.Atoi(minStr)
			if protection.RequiredPullRequestReviews.RequiredApprovingReviewCount < min {
				return &compliance.Result{
					RuleID:   rule.ID,
					RuleName: rule.Name,
					Repo:     repo.FullName,
					Status:   compliance.StatusFail,
					Severity: rule.Severity,
					Message:  fmt.Sprintf("required reviewers %d < %d", protection.RequiredPullRequestReviews.RequiredApprovingReviewCount, min),
				}, nil
			}
		}
	}

	return &compliance.Result{
		RuleID:   rule.ID,
		RuleName: rule.Name,
		Repo:     repo.FullName,
		Status:   compliance.StatusPass,
		Severity: rule.Severity,
		Message:  fmt.Sprintf("branch protection enabled on %s", branch),
	}, nil
}

func init() {
	compliance.Register(&branchProtectionChecker{})
}
