package notify

import (
	"strings"
	"testing"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
)

// — helpers ——————————————————————————————————————————————————————————————————

func makeResult(repo string, status compliance.Status, sev config.Severity, private bool) compliance.Result {
	return compliance.Result{
		RuleID:   "r1",
		RuleName: "Rule One",
		Repo:     repo,
		Status:   status,
		Severity: sev,
		Private:  private,
		Message:  "test message",
	}
}

// — splitRepo —————————————————————————————————————————————————————————————————

func TestSplitRepo_Valid(t *testing.T) {
	owner, repo, err := splitRepo("eukarya/myrepo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owner != "eukarya" || repo != "myrepo" {
		t.Errorf("got owner=%q repo=%q, want eukarya/myrepo", owner, repo)
	}
}

func TestSplitRepo_Invalid(t *testing.T) {
	cases := []string{"", "noslash", "/noop", "only/"}
	for _, c := range cases {
		_, _, err := splitRepo(c)
		if err == nil {
			t.Errorf("splitRepo(%q) should error", c)
		}
	}
}

func TestSplitRepo_DeepPath(t *testing.T) {
	// Only the first slash is the separator; rest goes to repo name.
	owner, repo, err := splitRepo("owner/repo/extra")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owner != "owner" || repo != "repo/extra" {
		t.Errorf("got %q/%q", owner, repo)
	}
}

// — visibilityLabel ——————————————————————————————————————————————————————————

func TestVisibilityLabel(t *testing.T) {
	pub := visibilityLabel(false)
	priv := visibilityLabel(true)

	if !strings.Contains(pub, "public") {
		t.Errorf("expected public label, got %q", pub)
	}
	if !strings.Contains(priv, "private") {
		t.Errorf("expected private label, got %q", priv)
	}
}

// — filterFailed ——————————————————————————————————————————————————————————————

func TestFilterFailed(t *testing.T) {
	results := []compliance.Result{
		makeResult("org/a", compliance.StatusFail, config.SeverityError, false),
		makeResult("org/a", compliance.StatusFail, config.SeverityWarning, false),
		makeResult("org/a", compliance.StatusFail, config.SeverityInfo, false),
		makeResult("org/a", compliance.StatusPass, config.SeverityError, false),
		makeResult("org/a", compliance.StatusSkip, config.SeverityError, false),
	}
	got := filterFailed(results)
	// Only error+warning failures should be returned.
	if len(got) != 2 {
		t.Errorf("expected 2 failures (error+warning), got %d", len(got))
	}
	for _, r := range got {
		if r.Status != compliance.StatusFail {
			t.Errorf("filterFailed returned non-failure: %v", r.Status)
		}
		if r.Severity == config.SeverityInfo {
			t.Error("filterFailed should exclude info severity failures")
		}
	}
}

func TestFilterFailed_Empty(t *testing.T) {
	if got := filterFailed(nil); len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

// — countRepos ————————————————————————————————————————————————————————————————

func TestCountRepos(t *testing.T) {
	results := []compliance.Result{
		makeResult("org/a", compliance.StatusPass, config.SeverityWarning, false),
		makeResult("org/a", compliance.StatusFail, config.SeverityError, false),
		makeResult("org/b", compliance.StatusPass, config.SeverityWarning, false),
	}
	if got := countRepos(results); got != 2 {
		t.Errorf("countRepos = %d, want 2", got)
	}
}

func TestCountRepos_Empty(t *testing.T) {
	if got := countRepos(nil); got != 0 {
		t.Errorf("countRepos(nil) = %d, want 0", got)
	}
}

// — splitIntoBatches —————————————————————————————————————————————————————————

func TestSplitIntoBatches_ShortBody(t *testing.T) {
	body := "short body"
	batches := splitIntoBatches(body)
	if len(batches) != 1 || batches[0] != body {
		t.Errorf("expected single batch unchanged, got %v", batches)
	}
}

func TestSplitIntoBatches_ExactLimit(t *testing.T) {
	body := strings.Repeat("a", githubMaxBodyLen)
	batches := splitIntoBatches(body)
	if len(batches) != 1 {
		t.Errorf("expected 1 batch for body at exact limit, got %d", len(batches))
	}
}

func TestSplitIntoBatches_OverLimit(t *testing.T) {
	// Build a body slightly over the limit with newlines so splitting is clean.
	line := strings.Repeat("x", 100) + "\n"
	body := strings.Repeat(line, (githubMaxBodyLen/len(line))+2)

	batches := splitIntoBatches(body)
	if len(batches) < 2 {
		t.Errorf("expected multiple batches for oversized body, got %d", len(batches))
	}
	// Verify no batch exceeds the limit.
	for i, b := range batches {
		if len(b) > githubMaxBodyLen {
			t.Errorf("batch %d exceeds limit: len=%d", i, len(b))
		}
	}
	// Verify all content is preserved.
	total := strings.Join(batches, "")
	if total != body {
		t.Error("reassembled batches do not match original body")
	}
}

// — buildConsolidatedBody ————————————————————————————————————————————————————

func TestBuildConsolidatedBody_AllPass(t *testing.T) {
	results := []compliance.Result{
		makeResult("org/a", compliance.StatusPass, config.SeverityWarning, false),
	}
	body := buildConsolidatedBody("myorg", results, "", config.Scope{})

	if !strings.Contains(body, gitCascadeMarker) {
		t.Error("expected git-cascade marker")
	}
	if !strings.Contains(body, "All compliance checks passed") {
		t.Error("expected all-pass message")
	}
}

func TestBuildConsolidatedBody_WithFailures(t *testing.T) {
	results := []compliance.Result{
		makeResult("org/a", compliance.StatusFail, config.SeverityError, false),
		makeResult("org/b", compliance.StatusPass, config.SeverityWarning, false),
	}
	body := buildConsolidatedBody("myorg", results, "", config.Scope{})

	if !strings.Contains(body, "org/a") {
		t.Error("expected failing repo in body")
	}
	// Passing repo must not appear as a failure section.
	if strings.Contains(body, "## `org/b`") {
		t.Error("passing repo should not appear as a failure section")
	}
}

func TestBuildConsolidatedBody_CILink(t *testing.T) {
	body := buildConsolidatedBody("myorg", nil, "https://ci.example.com/run/123", config.Scope{})
	if !strings.Contains(body, "https://ci.example.com/run/123") {
		t.Error("expected CI URL in body")
	}
}

func TestBuildConsolidatedBody_NoCILink(t *testing.T) {
	body := buildConsolidatedBody("myorg", nil, "", config.Scope{})
	if strings.Contains(body, "View CI run") {
		t.Error("expected no CI link when ciURL is empty")
	}
}

func TestBuildConsolidatedBody_PrivateRepoLabel(t *testing.T) {
	results := []compliance.Result{
		makeResult("org/private-api", compliance.StatusFail, config.SeverityError, true),
	}
	body := buildConsolidatedBody("myorg", results, "", config.Scope{})
	if !strings.Contains(body, "private") {
		t.Error("expected private label for private repo failure")
	}
}

func TestBuildConsolidatedBody_ScopeIncluded(t *testing.T) {
	scope := config.Scope{
		IncludeRepos: []string{"org/api"},
	}
	body := buildConsolidatedBody("myorg", nil, "", scope)
	if !strings.Contains(body, "org/api") {
		t.Error("expected scope repos to appear in body")
	}
}

// — buildPerRepoBody —————————————————————————————————————————————————————————

func TestBuildPerRepoBody_ContainsMarkerAndFailures(t *testing.T) {
	failures := []compliance.Result{
		makeResult("org/api", compliance.StatusFail, config.SeverityError, false),
	}
	body := buildPerRepoBody("org/api", failures)

	if !strings.Contains(body, gitCascadeMarker) {
		t.Error("expected git-cascade marker")
	}
	if !strings.Contains(body, "org/api") {
		t.Error("expected repo name in body")
	}
	if !strings.Contains(body, "r1") {
		t.Error("expected rule ID in body")
	}
}

func TestBuildPerRepoBody_PrivateLabel(t *testing.T) {
	failures := []compliance.Result{
		makeResult("org/api", compliance.StatusFail, config.SeverityError, true),
	}
	body := buildPerRepoBody("org/api", failures)
	if !strings.Contains(body, "private") {
		t.Error("expected private label")
	}
}

// — PostIssues mode validation ————————————————————————————————————————————————

func TestPostIssues_UnknownMode(t *testing.T) {
	cfg := config.IssuesConfig{Mode: "unknown"}
	_, err := PostIssues(t.Context(), nil, cfg, "org", nil, "", config.Scope{})
	if err == nil || !strings.Contains(err.Error(), "unknown issues mode") {
		t.Errorf("expected unknown mode error, got %v", err)
	}
}
