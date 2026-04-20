package checks

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/google/go-github/v84/github"
)

type branchProtectionChecker struct{}

func (c *branchProtectionChecker) ID() string { return "branch-protection" }

func (c *branchProtectionChecker) Check(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule) (*compliance.Result, error) {
	defaultBranch := repo.DefaultBranch
	if defaultBranch == "" {
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusSkip,
			Severity: rule.Severity,
			Message:  "no default branch set",
		}, nil
	}

	branches := []string{defaultBranch}
	for _, b := range rule.ListParams["additional_branches"] {
		if b != defaultBranch {
			branches = append(branches, b)
		}
	}

	var failures []string
	for _, branch := range branches {
		msg, err := checkBranchProtection(ctx, client, repo, rule, branch)
		if err != nil {
			return nil, err
		}
		if msg != "" {
			failures = append(failures, msg)
		}
	}

	if len(failures) > 0 {
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusFail,
			Severity: rule.Severity,
			Message:  strings.Join(failures, "; "),
		}, nil
	}

	checkedBranches := strings.Join(branches, ", ")
	return &compliance.Result{
		RuleID:   rule.ID,
		RuleName: rule.Name,
		Repo:     repo.FullName,
		Status:   compliance.StatusPass,
		Severity: rule.Severity,
		Message:  fmt.Sprintf("branch protection enabled on %s", checkedBranches),
	}, nil
}

// checkBranchProtection checks protection for a single branch.
// Returns a non-empty failure message if the branch is not adequately protected,
// an empty string on pass, or an error for unexpected API failures.
func checkBranchProtection(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule, branch string) (string, error) {
	// --- 1. Rulesets (modern GitHub) via GET /repos/{owner}/{repo}/rules/branches/{branch} ---
	// This returns the effective merged rules from all applicable rulesets
	// (repository, organization, enterprise) — no need to inspect individual rulesets.
	branchRules, statusCode, err := gh.GetBranchRules(ctx, client, repo.Owner, repo.Name, branch)
	if err != nil {
		return "", err
	}
	if statusCode == 0 && branchRulesActive(branchRules) {
		if msg := checkBranchRulesReviewParams(branchRules, rule); msg != "" {
			return fmt.Sprintf("[%s] %s", branch, msg), nil
		}
		return "", nil
	}
	// statusCode != 0 means the API is unavailable (e.g. 403 on older plans),
	// or branchRules is empty — fall through to legacy check.

	// --- 2. Legacy branch protection rules ---
	protection, statusCode, err := gh.GetBranchProtection(ctx, client, repo.Owner, repo.Name, branch)
	if err != nil {
		return "", err
	}
	if protection == nil {
		switch statusCode {
		case 404:
			return fmt.Sprintf("no branch protection or ruleset found for %s", branch), nil
		case 403:
			// API unavailable — treat as skip by returning empty (caller sees no failure).
			return "", nil
		default:
			return "", fmt.Errorf("fetching branch protection for %s/%s:%s: unexpected status %d", repo.Owner, repo.Name, branch, statusCode)
		}
	}

	if rule.Params["require_reviews"] == "true" {
		if protection.RequiredPullRequestReviews == nil {
			return fmt.Sprintf("[%s] pull request reviews not required", branch), nil
		}
		if minStr, ok := rule.Params["required_reviewers"]; ok {
			min, _ := strconv.Atoi(minStr)
			if protection.RequiredPullRequestReviews.RequiredApprovingReviewCount < min {
				return fmt.Sprintf("[%s] required reviewers %d < %d", branch, protection.RequiredPullRequestReviews.RequiredApprovingReviewCount, min), nil
			}
		}
	}

	return "", nil
}

// branchRulesActive returns true if the BranchRules response contains at least
// one rule, indicating the branch is covered by an active ruleset.
func branchRulesActive(r *github.BranchRules) bool {
	if r == nil {
		return false
	}
	return len(r.Creation) > 0 ||
		len(r.Update) > 0 ||
		len(r.Deletion) > 0 ||
		len(r.RequiredLinearHistory) > 0 ||
		len(r.RequiredSignatures) > 0 ||
		len(r.PullRequest) > 0 ||
		len(r.RequiredStatusChecks) > 0 ||
		len(r.NonFastForward) > 0 ||
		len(r.MergeQueue) > 0 ||
		len(r.RequiredDeployments) > 0 ||
		len(r.Workflows) > 0
}

// checkBranchRulesReviewParams validates require_reviews / required_reviewers
// against the effective PullRequest rules on the branch.
// Returns an empty string if the check passes.
func checkBranchRulesReviewParams(r *github.BranchRules, rule config.Rule) string {
	if rule.Params["require_reviews"] != "true" {
		return ""
	}
	if len(r.PullRequest) == 0 {
		return "pull request reviews not required (no PR rule in ruleset)"
	}
	if minStr, ok := rule.Params["required_reviewers"]; ok {
		min, _ := strconv.Atoi(minStr)
		// Use the highest required_approving_review_count across all PR rules.
		max := 0
		for _, pr := range r.PullRequest {
			if pr.Parameters.RequiredApprovingReviewCount > max {
				max = pr.Parameters.RequiredApprovingReviewCount
			}
		}
		if max < min {
			return fmt.Sprintf("required reviewers %d < %d", max, min)
		}
	}
	return ""
}

func init() {
	compliance.Register(&branchProtectionChecker{})
}
