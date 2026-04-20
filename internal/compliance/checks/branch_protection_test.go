package checks

import (
	"testing"

	"github.com/eukarya-inc/git-cascade/internal/config"
	"github.com/google/go-github/v84/github"
)

// — branchRulesActive —————————————————————————————————————————————————————————

func TestBranchRulesActive_Nil(t *testing.T) {
	if branchRulesActive(nil) {
		t.Error("expected false for nil BranchRules")
	}
}

func TestBranchRulesActive_Empty(t *testing.T) {
	if branchRulesActive(&github.BranchRules{}) {
		t.Error("expected false for empty BranchRules")
	}
}

func TestBranchRulesActive_WithPullRequest(t *testing.T) {
	rules := &github.BranchRules{
		PullRequest: []*github.PullRequestBranchRule{
			{Parameters: github.PullRequestRuleParameters{RequiredApprovingReviewCount: 1}},
		},
	}
	if !branchRulesActive(rules) {
		t.Error("expected true when PullRequest rules are set")
	}
}

func TestBranchRulesActive_WithDeletion(t *testing.T) {
	rules := &github.BranchRules{
		Deletion: []*github.BranchRuleMetadata{{}},
	}
	if !branchRulesActive(rules) {
		t.Error("expected true when Deletion rule is set")
	}
}

func TestBranchRulesActive_WithRequiredStatusChecks(t *testing.T) {
	rules := &github.BranchRules{
		RequiredStatusChecks: []*github.RequiredStatusChecksBranchRule{{}},
	}
	if !branchRulesActive(rules) {
		t.Error("expected true when RequiredStatusChecks rule is set")
	}
}

func TestBranchRulesActive_WithNonFastForward(t *testing.T) {
	rules := &github.BranchRules{
		NonFastForward: []*github.BranchRuleMetadata{{}},
	}
	if !branchRulesActive(rules) {
		t.Error("expected true when NonFastForward rule is set")
	}
}

// — checkBranchRulesReviewParams ——————————————————————————————————————————————

func TestCheckBranchRulesReviewParams_NoRequireReviews(t *testing.T) {
	rule := baseRule("branch-protection")
	// require_reviews not set → always passes.
	msg := checkBranchRulesReviewParams(&github.BranchRules{}, rule)
	if msg != "" {
		t.Errorf("expected empty message, got %q", msg)
	}
}

func TestCheckBranchRulesReviewParams_NoPRRule(t *testing.T) {
	rule := config.Rule{
		ID:       "branch-protection",
		Severity: config.SeverityError,
		Params:   map[string]string{"require_reviews": "true"},
	}
	msg := checkBranchRulesReviewParams(&github.BranchRules{}, rule)
	if msg == "" {
		t.Error("expected failure message when require_reviews=true but no PR rule")
	}
}

func TestCheckBranchRulesReviewParams_SufficientReviewers(t *testing.T) {
	rule := config.Rule{
		ID:       "branch-protection",
		Severity: config.SeverityError,
		Params:   map[string]string{"require_reviews": "true", "required_reviewers": "2"},
	}
	rules := &github.BranchRules{
		PullRequest: []*github.PullRequestBranchRule{
			{Parameters: github.PullRequestRuleParameters{RequiredApprovingReviewCount: 2}},
		},
	}
	msg := checkBranchRulesReviewParams(rules, rule)
	if msg != "" {
		t.Errorf("expected pass, got %q", msg)
	}
}

func TestCheckBranchRulesReviewParams_InsufficientReviewers(t *testing.T) {
	rule := config.Rule{
		ID:       "branch-protection",
		Severity: config.SeverityError,
		Params:   map[string]string{"require_reviews": "true", "required_reviewers": "3"},
	}
	rules := &github.BranchRules{
		PullRequest: []*github.PullRequestBranchRule{
			{Parameters: github.PullRequestRuleParameters{RequiredApprovingReviewCount: 1}},
		},
	}
	msg := checkBranchRulesReviewParams(rules, rule)
	if msg == "" {
		t.Error("expected failure when reviewers count is below required")
	}
}

func TestCheckBranchRulesReviewParams_MaxAcrossMultipleRules(t *testing.T) {
	// The highest count across multiple PR rules should be used.
	rule := config.Rule{
		ID:       "branch-protection",
		Severity: config.SeverityError,
		Params:   map[string]string{"require_reviews": "true", "required_reviewers": "2"},
	}
	rules := &github.BranchRules{
		PullRequest: []*github.PullRequestBranchRule{
			{Parameters: github.PullRequestRuleParameters{RequiredApprovingReviewCount: 1}},
			{Parameters: github.PullRequestRuleParameters{RequiredApprovingReviewCount: 3}},
		},
	}
	msg := checkBranchRulesReviewParams(rules, rule)
	if msg != "" {
		t.Errorf("expected pass (max=3 >= required=2), got %q", msg)
	}
}

func TestCheckBranchRulesReviewParams_NoRequiredReviewersParam(t *testing.T) {
	// require_reviews=true but no required_reviewers param → just checks PR rule exists.
	rule := config.Rule{
		ID:       "branch-protection",
		Severity: config.SeverityError,
		Params:   map[string]string{"require_reviews": "true"},
	}
	rules := &github.BranchRules{
		PullRequest: []*github.PullRequestBranchRule{
			{Parameters: github.PullRequestRuleParameters{RequiredApprovingReviewCount: 0}},
		},
	}
	msg := checkBranchRulesReviewParams(rules, rule)
	if msg != "" {
		t.Errorf("expected pass when no required_reviewers param, got %q", msg)
	}
}
