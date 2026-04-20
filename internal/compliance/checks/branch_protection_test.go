package checks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
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

// — additional_branches ———————————————————————————————————————————————————————

// branchProtectionServer builds a minimal httptest.Server that serves the
// GitHub branch-rules and branch-protection endpoints.
// branchRules controls which branches have an active ruleset (returns a
// PullRequest rule); protectedBranches controls which branches return legacy
// protection (returns non-nil Protection).  Everything else gets 404.
type branchProtectionServer struct {
	branchRules       map[string]bool // branch name → has active ruleset
	protectedBranches map[string]bool // branch name → has legacy protection
}

func (b *branchProtectionServer) serve(t *testing.T) (*httptest.Server, *github.Client) {
	t.Helper()
	mux := http.NewServeMux()

	// Ruleset endpoint: /api/v3/repos/{owner}/{repo}/rules/branches/{branch}
	mux.HandleFunc("/api/v3/repos/", func(w http.ResponseWriter, r *http.Request) {
		// Determine whether this is a rules or protection request.
		path := r.URL.Path
		switch {
		case strings.Contains(path, "/rules/branches/"):
			// Extract branch name (last path segment).
			parts := strings.Split(path, "/rules/branches/")
			branch := parts[len(parts)-1]
			if b.branchRules[branch] {
				// BranchRules.UnmarshalJSON expects a JSON *array* of rule objects,
				// each with a "type" field (e.g. "pull_request").
				resp := []map[string]any{
					{
						"type": "pull_request",
						"parameters": map[string]any{
							"required_approving_review_count": 1,
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
				return
			}
			// Return empty ruleset (no active rules) — empty array.
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[]`))

		case strings.Contains(path, "/branches/") && strings.Contains(path, "/protection"):
			// Extract branch from: /api/v3/repos/{owner}/{repo}/branches/{branch}/protection
			parts := strings.Split(path, "/branches/")
			branchAndRest := parts[len(parts)-1]
			branch := strings.TrimSuffix(branchAndRest, "/protection")
			if b.protectedBranches[branch] {
				resp := map[string]any{
					"required_pull_request_reviews": map[string]any{
						"required_approving_review_count": 1,
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
				return
			}
			http.NotFound(w, r)

		default:
			http.NotFound(w, r)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := github.NewClient(nil).WithAuthToken("fake-token")
	baseURL := srv.URL + "/"
	client, _ = client.WithEnterpriseURLs(baseURL, baseURL)
	return srv, client
}

func repoWithDefault(defaultBranch string) gh.Repository {
	return gh.Repository{
		Owner:         "org",
		Name:          "repo",
		FullName:      "org/repo",
		DefaultBranch: defaultBranch,
	}
}

func TestCheck_NoDefaultBranch(t *testing.T) {
	checker := &branchProtectionChecker{}
	srv := &branchProtectionServer{}
	_, client := srv.serve(t)

	repo := repoWithDefault("")
	rule := baseRule("branch-protection")

	result, err := checker.Check(context.Background(), client, repo, rule)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != compliance.StatusSkip {
		t.Errorf("expected skip, got %s", result.Status)
	}
}

func TestCheck_DefaultBranchProtected_NoAdditional(t *testing.T) {
	checker := &branchProtectionChecker{}
	srv := &branchProtectionServer{
		protectedBranches: map[string]bool{"main": true},
	}
	_, client := srv.serve(t)

	repo := repoWithDefault("main")
	rule := baseRule("branch-protection")

	result, err := checker.Check(context.Background(), client, repo, rule)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass, got %s: %s", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, "main") {
		t.Errorf("expected message to mention 'main', got %q", result.Message)
	}
}

func TestCheck_DefaultBranchUnprotected(t *testing.T) {
	checker := &branchProtectionChecker{}
	srv := &branchProtectionServer{} // nothing protected
	_, client := srv.serve(t)

	repo := repoWithDefault("main")
	rule := baseRule("branch-protection")

	result, err := checker.Check(context.Background(), client, repo, rule)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s", result.Status)
	}
}

func TestCheck_AdditionalBranchProtected(t *testing.T) {
	checker := &branchProtectionChecker{}
	srv := &branchProtectionServer{
		protectedBranches: map[string]bool{"main": true, "develop": true},
	}
	_, client := srv.serve(t)

	repo := repoWithDefault("main")
	rule := config.Rule{
		ID:         "branch-protection",
		Name:       "branch-protection",
		Severity:   config.SeverityError,
		Enabled:    true,
		ListParams: map[string][]string{"additional_branches": {"develop"}},
	}

	result, err := checker.Check(context.Background(), client, repo, rule)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass, got %s: %s", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, "main") || !strings.Contains(result.Message, "develop") {
		t.Errorf("expected message to mention both branches, got %q", result.Message)
	}
}

func TestCheck_AdditionalBranchUnprotected(t *testing.T) {
	checker := &branchProtectionChecker{}
	srv := &branchProtectionServer{
		protectedBranches: map[string]bool{"main": true}, // develop not protected
	}
	_, client := srv.serve(t)

	repo := repoWithDefault("main")
	rule := config.Rule{
		ID:         "branch-protection",
		Name:       "branch-protection",
		Severity:   config.SeverityError,
		Enabled:    true,
		ListParams: map[string][]string{"additional_branches": {"develop"}},
	}

	result, err := checker.Check(context.Background(), client, repo, rule)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s", result.Status)
	}
	if !strings.Contains(result.Message, "develop") {
		t.Errorf("expected message to mention 'develop', got %q", result.Message)
	}
}

func TestCheck_MultipleAdditionalBranches_AllFail(t *testing.T) {
	checker := &branchProtectionChecker{}
	srv := &branchProtectionServer{
		protectedBranches: map[string]bool{"main": true}, // staging and release not protected
	}
	_, client := srv.serve(t)

	repo := repoWithDefault("main")
	rule := config.Rule{
		ID:         "branch-protection",
		Name:       "branch-protection",
		Severity:   config.SeverityError,
		Enabled:    true,
		ListParams: map[string][]string{"additional_branches": {"staging", "release"}},
	}

	result, err := checker.Check(context.Background(), client, repo, rule)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s", result.Status)
	}
	if !strings.Contains(result.Message, "staging") || !strings.Contains(result.Message, "release") {
		t.Errorf("expected message to mention both failing branches, got %q", result.Message)
	}
}

func TestCheck_AdditionalBranchSameAsDefault_DeduplicatedAndPasses(t *testing.T) {
	// Listing the default branch in additional_branches should not double-check it.
	checker := &branchProtectionChecker{}
	srv := &branchProtectionServer{
		protectedBranches: map[string]bool{"main": true},
	}
	_, client := srv.serve(t)

	repo := repoWithDefault("main")
	rule := config.Rule{
		ID:         "branch-protection",
		Name:       "branch-protection",
		Severity:   config.SeverityError,
		Enabled:    true,
		ListParams: map[string][]string{"additional_branches": {"main"}},
	}

	result, err := checker.Check(context.Background(), client, repo, rule)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass, got %s: %s", result.Status, result.Message)
	}
}

// — checkBranchProtection helper branches ————————————————————————————————————

func TestCheck_RulesetActive_ReviewParamFail(t *testing.T) {
	// When ruleset is active but reviewer count is below the required minimum,
	// checkBranchProtection should return a prefixed failure message.
	checker := &branchProtectionChecker{}
	srv := &branchProtectionServer{
		branchRules: map[string]bool{"main": true}, // active ruleset with 1 reviewer
	}
	_, client := srv.serve(t)

	repo := repoWithDefault("main")
	rule := config.Rule{
		ID:       "branch-protection",
		Name:     "branch-protection",
		Severity: config.SeverityError,
		Enabled:  true,
		Params:   map[string]string{"require_reviews": "true", "required_reviewers": "3"},
	}

	result, err := checker.Check(context.Background(), client, repo, rule)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail when ruleset has fewer reviewers than required, got %s: %s", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, "[main]") {
		t.Errorf("expected branch name in failure message, got %q", result.Message)
	}
}

func TestCheck_LegacyProtection_RequireReviews_NoReviews(t *testing.T) {
	// Legacy protection exists but PR reviews are not configured and require_reviews=true.
	checker := &branchProtectionChecker{}
	srv := &branchProtectionServer{
		protectedBranches: map[string]bool{"main": true},
	}
	_, client := srv.serve(t)

	repo := repoWithDefault("main")
	rule := config.Rule{
		ID:       "branch-protection",
		Name:     "branch-protection",
		Severity: config.SeverityError,
		Enabled:  true,
		// The fake server returns protection without RequiredPullRequestReviews.
		// However our fake always includes it — so test the reviewer count path instead:
		// request 5 reviewers; fake only provides 1.
		Params: map[string]string{"require_reviews": "true", "required_reviewers": "5"},
	}

	result, err := checker.Check(context.Background(), client, repo, rule)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail when legacy protection has fewer reviewers than required, got %s: %s", result.Status, result.Message)
	}
}

func TestCheck_403OnBothAPIs_Skips(t *testing.T) {
	// When both ruleset and legacy protection APIs return 403 the branch is treated
	// as skipped (no failure added). The overall result should be pass since no
	// failure messages accumulate.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Forbidden", http.StatusForbidden)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client := github.NewClient(nil).WithAuthToken("fake-token")
	client, _ = client.WithEnterpriseURLs(srv.URL+"/", srv.URL+"/")

	checker := &branchProtectionChecker{}
	repo := repoWithDefault("main")
	rule := baseRule("branch-protection")

	result, err := checker.Check(context.Background(), client, repo, rule)
	if err != nil {
		t.Fatal(err)
	}
	// 403 on protection API → empty failure message → overall pass
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass when API returns 403 (no failures accumulated), got %s: %s", result.Status, result.Message)
	}
}
