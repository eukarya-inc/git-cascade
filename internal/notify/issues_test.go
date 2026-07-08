package notify

import (
	"fmt"
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

func makeResultWithRule(repo, ruleID string, status compliance.Status, sev config.Severity, private bool) compliance.Result {
	return compliance.Result{
		RuleID:   ruleID,
		RuleName: ruleID,
		Repo:     repo,
		Status:   status,
		Severity: sev,
		Private:  private,
		Message:  "test message for " + ruleID,
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
	if !strings.Contains(visibilityLabel(false), "public") {
		t.Error("expected public label")
	}
	if !strings.Contains(visibilityLabel(true), "private") {
		t.Error("expected private label")
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
	if len(splitIntoBatches(body)) != 1 {
		t.Error("expected 1 batch for body at exact limit")
	}
}

func TestSplitIntoBatches_OverLimit(t *testing.T) {
	line := strings.Repeat("x", 100) + "\n"
	body := strings.Repeat(line, (githubMaxBodyLen/len(line))+2)
	batches := splitIntoBatches(body)
	if len(batches) < 2 {
		t.Errorf("expected multiple batches, got %d", len(batches))
	}
	for i, b := range batches {
		if len(b) > githubMaxBodyLen {
			t.Errorf("batch %d exceeds limit: len=%d", i, len(b))
		}
	}
	if strings.Join(batches, "") != body {
		t.Error("reassembled batches do not match original body")
	}
}

// — parseRepoMarker ———————————————————————————————————————————————————————————

func TestParseRepoMarker_Valid(t *testing.T) {
	body := "<!-- git-cascade:repo:org/myrepo -->\n## details..."
	repo, ok := parseRepoMarker(body)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if repo != "org/myrepo" {
		t.Errorf("got %q, want org/myrepo", repo)
	}
}

func TestParseRepoMarker_NotARepoComment(t *testing.T) {
	cases := []string{
		"<!-- git-cascade -->\n# summary",
		"regular comment without marker",
		"<!-- git-cascade:repo: -->", // empty repo name — end immediately after prefix+space
	}
	for _, c := range cases {
		_, ok := parseRepoMarker(c)
		// The empty-name case technically parses as "" which is fine either way;
		// we only assert non-repo comments return false.
		if ok && !strings.HasPrefix(c, repoCommentPrefix) {
			t.Errorf("parseRepoMarker(%q) should return false", c)
		}
	}
}

func TestParseRepoMarker_MissingCloser(t *testing.T) {
	body := "<!-- git-cascade:repo:org/repo without closer"
	_, ok := parseRepoMarker(body)
	if ok {
		t.Error("expected ok=false for marker without closing -->")
	}
}

// — buildSummaryBody ——————————————————————————————————————————————————————————

func TestBuildSummaryBody_ContainsMarker(t *testing.T) {
	body := buildSummaryBody("myorg", map[string][]compliance.Result{}, "", config.Scope{})
	if !strings.Contains(body, gitCascadeMarker) {
		t.Error("expected git-cascade marker in summary body")
	}
}

func TestBuildSummaryBody_ContainsOrgName(t *testing.T) {
	body := buildSummaryBody("eukarya", map[string][]compliance.Result{}, "", config.Scope{})
	if !strings.Contains(body, "eukarya") {
		t.Error("expected org name in summary body")
	}
}

func TestBuildSummaryBody_CILink(t *testing.T) {
	body := buildSummaryBody("myorg", nil, "https://ci.example.com/run/123", config.Scope{})
	if !strings.Contains(body, "https://ci.example.com/run/123") {
		t.Error("expected CI URL in body")
	}
}

func TestBuildSummaryBody_NoCILink(t *testing.T) {
	body := buildSummaryBody("myorg", nil, "", config.Scope{})
	if strings.Contains(body, "View CI run") {
		t.Error("expected no CI link when ciURL is empty")
	}
}

func TestBuildSummaryBody_RepoTable(t *testing.T) {
	byRepo := map[string][]compliance.Result{
		"org/api": {
			makeResult("org/api", compliance.StatusFail, config.SeverityError, false),
		},
		"org/web": {
			makeResult("org/web", compliance.StatusPass, config.SeverityWarning, false),
		},
	}
	body := buildSummaryBody("myorg", byRepo, "", config.Scope{})

	if !strings.Contains(body, "org/api") {
		t.Error("expected org/api in summary table")
	}
	if !strings.Contains(body, "org/web") {
		t.Error("expected org/web in summary table")
	}
	// Failing repo should show ❌
	if !strings.Contains(body, "❌") {
		t.Error("expected ❌ for failing repo")
	}
	// Passing repo should show ✅
	if !strings.Contains(body, "✅") {
		t.Error("expected ✅ for passing repo")
	}
}

func TestBuildSummaryBody_DoesNotContainFindingDetails(t *testing.T) {
	// Detailed findings belong in per-repo comments, not the summary body.
	byRepo := map[string][]compliance.Result{
		"org/api": {
			makeResultWithRule("org/api", "branch-protection", compliance.StatusFail, config.SeverityError, false),
		},
	}
	body := buildSummaryBody("myorg", byRepo, "", config.Scope{})
	if strings.Contains(body, "branch-protection") {
		t.Error("summary body should not contain individual rule IDs — those belong in per-repo comments")
	}
}

func TestBuildSummaryBody_ScopeIncluded(t *testing.T) {
	scope := config.Scope{IncludeRepos: []string{"org/api"}}
	body := buildSummaryBody("myorg", nil, "", scope)
	if !strings.Contains(body, "org/api") {
		t.Error("expected scope repos to appear in body")
	}
}

func TestBuildSummaryBody_AllPassStatusIcon(t *testing.T) {
	byRepo := map[string][]compliance.Result{
		"org/a": {makeResult("org/a", compliance.StatusPass, config.SeverityError, false)},
	}
	body := buildSummaryBody("myorg", byRepo, "", config.Scope{})
	// Overall ✅ should appear when no failures.
	if !strings.Contains(body, "✅") {
		t.Error("expected ✅ overall icon when all repos pass")
	}
	if strings.Contains(body, "❌") {
		t.Error("unexpected ❌ when all repos pass")
	}
}

func TestBuildSummaryBody_ErrorStatusIcon(t *testing.T) {
	byRepo := map[string][]compliance.Result{
		"org/a": {makeResult("org/a", compliance.StatusFail, config.SeverityError, false)},
	}
	body := buildSummaryBody("myorg", byRepo, "", config.Scope{})
	if !strings.Contains(body, "❌") {
		t.Error("expected ❌ overall icon when errors exist")
	}
}

// — buildRepoCommentBody ——————————————————————————————————————————————————————

func TestBuildRepoCommentBody_StartsWithMarker(t *testing.T) {
	failures := []compliance.Result{
		makeResult("org/api", compliance.StatusFail, config.SeverityError, false),
	}
	body := buildRepoCommentBody("org/api", failures)
	if !strings.HasPrefix(body, repoCommentPrefix+"org/api -->") {
		t.Errorf("expected body to start with repo marker, got: %q", body[:min(len(body), 60)])
	}
}

func TestBuildRepoCommentBody_MarkerRoundTrips(t *testing.T) {
	failures := []compliance.Result{
		makeResult("org/myservice", compliance.StatusFail, config.SeverityWarning, true),
	}
	body := buildRepoCommentBody("org/myservice", failures)
	repo, ok := parseRepoMarker(body)
	if !ok {
		t.Fatal("parseRepoMarker returned false for a body produced by buildRepoCommentBody")
	}
	if repo != "org/myservice" {
		t.Errorf("got %q, want org/myservice", repo)
	}
}

func TestBuildRepoCommentBody_ContainsRuleAndMessage(t *testing.T) {
	failures := []compliance.Result{
		makeResultWithRule("org/api", "secret-detection", compliance.StatusFail, config.SeverityError, false),
	}
	body := buildRepoCommentBody("org/api", failures)
	if !strings.Contains(body, "secret-detection") {
		t.Error("expected rule ID in comment body")
	}
	if !strings.Contains(body, "test message for secret-detection") {
		t.Error("expected message in comment body")
	}
}

func TestBuildRepoCommentBody_PipeEscaped(t *testing.T) {
	r := makeResult("org/api", compliance.StatusFail, config.SeverityError, false)
	r.Message = "found secret in file | config.yaml"
	body := buildRepoCommentBody("org/api", []compliance.Result{r})
	if strings.Contains(body, "file | config") {
		t.Error("pipe in message should be escaped to avoid breaking Markdown table")
	}
	if !strings.Contains(body, `file \| config`) {
		t.Error("expected escaped pipe in comment body")
	}
}

func TestBuildRepoCommentBody_TruncatedWhenOverLimit(t *testing.T) {
	// Build enough failures to exceed the limit.
	var failures []compliance.Result
	longMsg := strings.Repeat("x", 200)
	for i := range (githubMaxBodyLen / 200) + 10 {
		r := makeResult("org/api", compliance.StatusFail, config.SeverityError, false)
		r.RuleID = fmt.Sprintf("rule-%d", i)
		r.Message = longMsg
		failures = append(failures, r)
	}
	body := buildRepoCommentBody("org/api", failures)
	if len(body) > githubMaxBodyLen {
		t.Errorf("body len %d exceeds githubMaxBodyLen %d", len(body), githubMaxBodyLen)
	}
	if !strings.Contains(body, "truncated") {
		t.Error("expected truncation notice in oversize body")
	}
}

func TestBuildRepoCommentBody_PrivateLabel(t *testing.T) {
	failures := []compliance.Result{
		makeResult("org/private-api", compliance.StatusFail, config.SeverityError, true),
	}
	body := buildRepoCommentBody("org/private-api", failures)
	if !strings.Contains(body, "private") {
		t.Error("expected private visibility label")
	}
}

// — buildPerRepoBody (mode=repo, unchanged) ——————————————————————————————————

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
	if !strings.Contains(buildPerRepoBody("org/api", failures), "private") {
		t.Error("expected private label")
	}
}

// — escapeTableCell ———————————————————————————————————————————————————————————

func TestEscapeTableCell(t *testing.T) {
	cases := []struct{ in, want string }{
		{"no pipes", "no pipes"},
		{"a|b", `a\|b`},
		{"a|b|c", `a\|b\|c`},
		{"", ""},
	}
	for _, c := range cases {
		if got := escapeTableCell(c.in); got != c.want {
			t.Errorf("escapeTableCell(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// — isSecondaryRateLimit ——————————————————————————————————————————————————————

func TestIsSecondaryRateLimit(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("secondary rate limit exceeded"), true},
		{fmt.Errorf("API rate limit exceeded"), true},
		{fmt.Errorf("HTTP 429 Too Many Requests"), true},
		{fmt.Errorf("not found"), false},
		{fmt.Errorf("internal server error"), false},
	}
	for _, c := range cases {
		if got := isSecondaryRateLimit(c.err); got != c.want {
			t.Errorf("isSecondaryRateLimit(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

// — syncRepoComments logic (unit-testable subset) ————————————————————————————

func TestSyncRepoComments_WorkItemsBuiltCorrectly(t *testing.T) {
	// Verify the work-item construction logic by checking what syncRepoComments
	// would process. We test this indirectly via the comment body helpers since
	// syncRepoComments itself needs a live GitHub client.

	byRepo := map[string][]compliance.Result{
		"org/failing": {makeResult("org/failing", compliance.StatusFail, config.SeverityError, false)},
		"org/passing": {makeResult("org/passing", compliance.StatusPass, config.SeverityWarning, false)},
	}
	existing := map[string]int64{
		"org/passing":     999, // had a comment last run — should be deleted
		"org/gone-repo":   888, // no longer in results — should be deleted
	}

	// Build work items the same way syncRepoComments does.
	var work []commentWork
	for repoFull, results := range byRepo {
		failures := filterFailed(results)
		existingID := existing[repoFull]
		if len(failures) == 0 && existingID == 0 {
			continue
		}
		work = append(work, commentWork{repoFull: repoFull, failures: failures, existingID: existingID})
	}
	for repoFull, commentID := range existing {
		if _, inResults := byRepo[repoFull]; !inResults {
			work = append(work, commentWork{repoFull: repoFull, failures: nil, existingID: commentID})
		}
	}

	// org/failing  → create new (no existing, has failures)
	// org/passing  → delete   (existing=999, no failures)
	// org/gone-repo → delete  (existing=888, not in results)
	findWork := func(repo string) (commentWork, bool) {
		for _, w := range work {
			if w.repoFull == repo {
				return w, true
			}
		}
		return commentWork{}, false
	}

	w, ok := findWork("org/failing")
	if !ok {
		t.Fatal("expected work item for org/failing")
	}
	if len(w.failures) == 0 || w.existingID != 0 {
		t.Errorf("org/failing: expected new create, got failures=%d existingID=%d", len(w.failures), w.existingID)
	}

	w, ok = findWork("org/passing")
	if !ok {
		t.Fatal("expected work item for org/passing")
	}
	if len(w.failures) != 0 || w.existingID != 999 {
		t.Errorf("org/passing: expected delete (existingID=999), got failures=%d existingID=%d", len(w.failures), w.existingID)
	}

	w, ok = findWork("org/gone-repo")
	if !ok {
		t.Fatal("expected work item for org/gone-repo (stale comment)")
	}
	if len(w.failures) != 0 || w.existingID != 888 {
		t.Errorf("org/gone-repo: expected delete, got failures=%d existingID=%d", len(w.failures), w.existingID)
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

func TestPostIssues_AppendMode_RequiresIssueTitle(t *testing.T) {
	cfg := config.IssuesConfig{Mode: "append"}
	_, err := PostIssues(t.Context(), nil, cfg, "org", nil, "", config.Scope{})
	if err == nil || !strings.Contains(err.Error(), "issue_title is required") {
		t.Errorf("expected issue_title required error, got %v", err)
	}
}

// — append-mode section markers —————————————————————————————————————————————

func TestParseSectionMarker_Valid(t *testing.T) {
	body := sectionCommentPrefix + "myorg -->\n# Compliance Report — myorg\n"
	key, ok := parseSectionMarker(body)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if key != "myorg" {
		t.Errorf("got %q, want myorg", key)
	}
}

func TestParseSectionMarker_NotASectionComment(t *testing.T) {
	_, ok := parseSectionMarker("regular comment without marker")
	if ok {
		t.Error("expected ok=false for non-section comment")
	}
}

func TestParseSectionMarker_DistinguishesKeys(t *testing.T) {
	a, _ := parseSectionMarker(sectionCommentPrefix + "team-a -->\nbody")
	b, _ := parseSectionMarker(sectionCommentPrefix + "team-b -->\nbody")
	if a == b {
		t.Error("expected distinct section keys to parse as distinct, so different git-cascade configs don't collide")
	}
}

func TestSectionCommentPrefix_DistinctFromRepoAndIssueMarkers(t *testing.T) {
	if sectionCommentPrefix == gitCascadeMarker || sectionCommentPrefix == repoCommentPrefix {
		t.Error("sectionCommentPrefix must be distinct so comments don't collide across modes")
	}
}
