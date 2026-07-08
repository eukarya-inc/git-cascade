package notify

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	"github.com/google/go-github/v84/github"
)

const issueTitle = "[COMPLIANCE] non-compliant item findings"
const issueTitlePerRepo = "[COMPLIANCE] non-compliant item findings"
const gitCascadeMarker = "<!-- git-cascade -->"

// repoCommentPrefix is the prefix used to mark per-repo detail comments.
// The full marker is repoCommentPrefix + repoFullName + " -->".
const repoCommentPrefix = "<!-- git-cascade:repo:"

// githubMaxBodyLen is GitHub's hard limit for issue bodies and comments.
const githubMaxBodyLen = 65536

// consolidatedCommentWorkers is the number of concurrent goroutines used when
// creating or updating per-repo detail comments on the consolidated issue.
const consolidatedCommentWorkers = 3

// PostIssues creates or updates GitHub Issues with scan findings.
// mode="compliance": one consolidated issue in the compliance repo.
// mode="repo":       one issue per scanned repo that has failures, posted in that repo.
// mode="append":     one comment on an existing (or bootstrapped) shared issue,
//                    identified by cfg.IssueTitle, so multiple scanning tools
//                    can report into the same integrated issue page.
// ciURL is an optional link to the CI job run embedded in the issue body.
// Returns the HTML URL of the upserted issue for mode=compliance/append (empty string for mode=repo).
func PostIssues(ctx context.Context, client *github.Client, cfg config.IssuesConfig, org string, results []compliance.Result, ciURL string, scope config.Scope) (string, error) {
	switch cfg.Mode {
	case "repo":
		return "", postPerRepoIssues(ctx, client, cfg, results)
	case "compliance", "":
		return postConsolidatedIssue(ctx, client, cfg, org, results, ciURL, scope)
	case "append":
		return postAppendIssue(ctx, client, cfg, org, results, ciURL, scope)
	default:
		return "", fmt.Errorf("unknown issues mode %q (must be \"compliance\", \"repo\", or \"append\")", cfg.Mode)
	}
}

// postConsolidatedIssue manages a single issue in the compliance repo.
//
// Structure:
//   - Issue body  — summary: scan metadata, overall counts, per-repo status table.
//   - Comments    — one comment per repository that has failures, each clearly
//     marked with <!-- git-cascade:repo:owner/repo --> so they can be
//     individually identified and upserted across runs.
//
// On each run:
//  1. The issue body (summary) is updated.
//  2. Existing repo comments are loaded and matched by marker.
//  3. Repos still failing get their comment created or updated.
//  4. Repos that are now passing get their stale comment deleted.
//
// Returns the HTML URL of the upserted issue.
func postConsolidatedIssue(ctx context.Context, client *github.Client, cfg config.IssuesConfig, org string, results []compliance.Result, ciURL string, scope config.Scope) (string, error) {
	repoRef := cfg.ComplianceRepo
	if repoRef == "" {
		repoRef = org + "/compliance"
	}
	owner, repo, err := splitRepo(repoRef)
	if err != nil {
		return "", err
	}

	byRepo := groupByRepoSorted(results)

	// Build and upsert the summary issue body.
	summaryBody := buildSummaryBody(org, byRepo, ciURL, scope, cfg.HeaderText)
	issueNumber, htmlURL, err := upsertIssue(ctx, client, owner, repo, issueTitle, summaryBody, cfg.Labels)
	if err != nil {
		return "", err
	}

	// Load all existing git-cascade repo comments on this issue.
	existing, err := loadRepoComments(ctx, client, owner, repo, issueNumber)
	if err != nil {
		return "", err
	}

	// Fan out comment upserts concurrently, then clean up stale comments.
	if err := syncRepoComments(ctx, client, owner, repo, issueNumber, byRepo, existing); err != nil {
		return "", err
	}

	return htmlURL, nil
}

// postPerRepoIssues creates or updates one issue per repository that has failures.
func postPerRepoIssues(ctx context.Context, client *github.Client, cfg config.IssuesConfig, results []compliance.Result) error {
	byRepo := groupByRepo(results)
	for repoFull, repoResults := range byRepo {
		failures := filterFailed(repoResults)
		if len(failures) == 0 {
			continue
		}
		owner, repo, err := splitRepo(repoFull)
		if err != nil {
			return err
		}
		body := buildPerRepoBody(repoFull, failures)
		if _, _, err := upsertIssue(ctx, client, owner, repo, issueTitlePerRepo, body, cfg.Labels); err != nil {
			return fmt.Errorf("posting issue to %s: %w", repoFull, err)
		}
	}
	return nil
}

// sectionCommentPrefix marks the comment git-cascade owns on a shared
// (mode=append) issue. The full marker is sectionCommentPrefix + sectionKey +
// " -->". Keying by section lets multiple git-cascade configs/orgs post into
// the same shared issue, each owning a distinct comment, without clobbering
// each other — and reruns of the same section edit it in place instead of
// piling up new comments each scan.
const sectionCommentPrefix = "<!-- git-cascade:section:"

// parseSectionMarker extracts the section key from a comment that starts
// with the section marker. Returns ("", false) if the comment isn't one of
// git-cascade's section comments.
func parseSectionMarker(body string) (string, bool) {
	if !strings.HasPrefix(body, sectionCommentPrefix) {
		return "", false
	}
	rest := body[len(sectionCommentPrefix):]
	key, _, found := strings.Cut(rest, " -->")
	if !found {
		return "", false
	}
	return key, true
}

// postAppendIssue upserts a single idempotent comment, scoped to cfg.SectionKey,
// on an existing (or bootstrapped) issue identified by cfg.IssueTitle, without
// touching the issue body — that's owned by whichever tool created it, or left
// minimal if git-cascade creates it. This lets multiple scanning tools (and
// multiple git-cascade configs/orgs) each report into their own comment on one
// shared, integrated issue.
func postAppendIssue(ctx context.Context, client *github.Client, cfg config.IssuesConfig, org string, results []compliance.Result, ciURL string, scope config.Scope) (string, error) {
	if cfg.IssueTitle == "" {
		return "", fmt.Errorf("issue_title is required for issues mode \"append\"")
	}
	sectionKey := cfg.SectionKey
	if sectionKey == "" {
		sectionKey = org
	}

	repoRef := cfg.ComplianceRepo
	if repoRef == "" {
		repoRef = org + "/compliance"
	}
	owner, repo, err := splitRepo(repoRef)
	if err != nil {
		return "", err
	}

	issueNumber, htmlURL, err := findOrCreateIssueByTitle(ctx, client, owner, repo, cfg.IssueTitle, cfg.Labels)
	if err != nil {
		return "", err
	}

	marker := sectionCommentPrefix + sectionKey + " -->"
	byRepo := groupByRepoSorted(results)
	body := buildSummaryBody(org, byRepo, ciURL, scope, "")
	body = strings.TrimPrefix(body, gitCascadeMarker+"\n")
	body = strings.TrimPrefix(body, fmt.Sprintf("# Compliance Report — %s\n\n", org))
	body = marker + "\n" + body
	body += buildFindingsDetail(byRepo)
	if len(body) > githubMaxBodyLen {
		cut := strings.LastIndex(body[:githubMaxBodyLen-100], "\n")
		if cut < 0 {
			cut = githubMaxBodyLen - 100
		}
		body = body[:cut] + "\n\n_… truncated — too many findings to display in a single comment._\n"
	}

	existingID, err := findSectionComment(ctx, client, owner, repo, issueNumber, sectionKey)
	if err != nil {
		return "", err
	}
	if existingID != 0 {
		if _, _, err := client.Issues.EditComment(ctx, owner, repo, existingID, &github.IssueComment{Body: &body}); err != nil {
			return "", fmt.Errorf("updating comment on #%d in %s/%s: %w", issueNumber, owner, repo, err)
		}
	} else {
		if _, _, err := client.Issues.CreateComment(ctx, owner, repo, issueNumber, &github.IssueComment{Body: &body}); err != nil {
			return "", fmt.Errorf("creating comment on #%d in %s/%s: %w", issueNumber, owner, repo, err)
		}
	}

	return htmlURL, nil
}

// findOrCreateIssueByTitle finds an open issue with an exact title match,
// regardless of who created it or what marker its body carries. If none is
// found, it creates a bare issue with that title so git-cascade can run
// before or after the other tools sharing this issue.
func findOrCreateIssueByTitle(ctx context.Context, client *github.Client, owner, repo, title string, labels []string) (int, string, error) {
	opts := &github.IssueListByRepoOptions{
		State:       "open",
		ListOptions: github.ListOptions{PerPage: 100},
	}
	for {
		issues, resp, err := client.Issues.ListByRepo(ctx, owner, repo, opts)
		if err != nil {
			return 0, "", fmt.Errorf("listing issues for %s/%s: %w", owner, repo, err)
		}
		for _, issue := range issues {
			if issue.GetTitle() == title {
				return issue.GetNumber(), issue.GetHTMLURL(), nil
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.ListOptions.Page = resp.NextPage
	}

	if labels == nil {
		labels = []string{}
	}
	body := "Integrated findings issue, shared across scanning tools.\n"
	issue, _, err := client.Issues.Create(ctx, owner, repo, &github.IssueRequest{
		Title:  &title,
		Body:   &body,
		Labels: &labels,
	})
	if err != nil {
		return 0, "", fmt.Errorf("creating shared issue in %s/%s: %w", owner, repo, err)
	}
	return issue.GetNumber(), issue.GetHTMLURL(), nil
}

// findSectionComment returns the ID of the comment owned by the given section
// key on the issue, or 0 if that section hasn't posted one yet.
func findSectionComment(ctx context.Context, client *github.Client, owner, repo string, issueNumber int, sectionKey string) (int64, error) {
	opts := &github.IssueListCommentsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}
	for {
		comments, resp, err := client.Issues.ListComments(ctx, owner, repo, issueNumber, opts)
		if err != nil {
			return 0, fmt.Errorf("listing comments on #%d in %s/%s: %w", issueNumber, owner, repo, err)
		}
		for _, c := range comments {
			if key, ok := parseSectionMarker(c.GetBody()); ok && key == sectionKey {
				return c.GetID(), nil
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.ListOptions.Page = resp.NextPage
	}
	return 0, nil
}

// loadRepoComments fetches all comments on the issue and returns a map from
// repo full name to comment ID for comments that carry a repo marker.
func loadRepoComments(ctx context.Context, client *github.Client, owner, repo string, issueNumber int) (map[string]int64, error) {
	out := make(map[string]int64)
	opts := &github.IssueListCommentsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}
	for {
		comments, resp, err := client.Issues.ListComments(ctx, owner, repo, issueNumber, opts)
		if err != nil {
			return nil, fmt.Errorf("listing comments on #%d in %s/%s: %w", issueNumber, owner, repo, err)
		}
		for _, c := range comments {
			if r, ok := parseRepoMarker(c.GetBody()); ok {
				out[r] = c.GetID()
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.ListOptions.Page = resp.NextPage
	}
	return out, nil
}

// parseRepoMarker extracts the repo full name from a comment that starts with
// the per-repo marker. Returns ("", false) if the comment is not a repo comment.
func parseRepoMarker(body string) (string, bool) {
	if !strings.HasPrefix(body, repoCommentPrefix) {
		return "", false
	}
	rest := body[len(repoCommentPrefix):]
	name, _, found := strings.Cut(rest, " -->")
	if !found {
		return "", false
	}
	return name, true
}

// commentWork is a unit of work for the comment worker pool.
type commentWork struct {
	repoFull   string
	failures   []compliance.Result
	existingID int64 // 0 = create new; >0 = edit this comment ID
}

// syncRepoComments creates/updates comments for repos with failures and deletes
// comments for repos that are now passing, using a bounded worker pool with
// exponential backoff on secondary rate limits.
func syncRepoComments(ctx context.Context, client *github.Client, owner, repo string, issueNumber int, byRepo map[string][]compliance.Result, existing map[string]int64) error {
	// Build work items: one per repo that has failures OR had a stale comment.
	var work []commentWork
	for repoFull, results := range byRepo {
		failures := filterFailed(results)
		existingID := existing[repoFull]
		if len(failures) == 0 && existingID == 0 {
			continue // nothing to do
		}
		work = append(work, commentWork{
			repoFull:   repoFull,
			failures:   failures,
			existingID: existingID,
		})
	}
	// Also queue deletions for repos that had a comment but are no longer in byRepo.
	for repoFull, commentID := range existing {
		if _, inResults := byRepo[repoFull]; !inResults {
			work = append(work, commentWork{
				repoFull:   repoFull,
				failures:   nil,
				existingID: commentID,
			})
		}
	}

	if len(work) == 0 {
		return nil
	}

	// Sort for deterministic ordering (helps with tests and audit logs).
	sort.Slice(work, func(i, j int) bool { return work[i].repoFull < work[j].repoFull })

	jobs := make(chan commentWork, len(work))
	for _, w := range work {
		jobs <- w
	}
	close(jobs)

	errs := make(chan error, len(work))
	var wg sync.WaitGroup
	for range consolidatedCommentWorkers {
		wg.Go(func() {
			for w := range jobs {
				if err := processCommentWork(ctx, client, owner, repo, issueNumber, w); err != nil {
					errs <- err
				}
			}
		})
	}
	wg.Wait()
	close(errs)

	// Return the first error encountered, if any.
	for err := range errs {
		return err
	}
	return nil
}

// processCommentWork handles a single comment create / update / delete with
// exponential backoff for secondary rate limits.
func processCommentWork(ctx context.Context, client *github.Client, owner, repo string, issueNumber int, w commentWork) error {
	const maxAttempts = 5
	backoff := 2 * time.Second

	for attempt := range maxAttempts {
		var err error
		if len(w.failures) == 0 {
			// Repo is now passing — delete stale comment.
			_, err = client.Issues.DeleteComment(ctx, owner, repo, w.existingID)
		} else if w.existingID != 0 {
			// Update existing comment.
			body := buildRepoCommentBody(w.repoFull, w.failures)
			_, _, err = client.Issues.EditComment(ctx, owner, repo, w.existingID, &github.IssueComment{Body: &body})
		} else {
			// Create new comment.
			body := buildRepoCommentBody(w.repoFull, w.failures)
			_, _, err = client.Issues.CreateComment(ctx, owner, repo, issueNumber, &github.IssueComment{Body: &body})
		}

		if err == nil {
			return nil
		}
		if !isSecondaryRateLimit(err) || attempt == maxAttempts-1 {
			return fmt.Errorf("comment operation for %s on #%d in %s/%s: %w", w.repoFull, issueNumber, owner, repo, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
			backoff *= 2
		}
	}
	return nil
}

// isSecondaryRateLimit returns true when err looks like a GitHub secondary rate
// limit response (HTTP 403 "secondary rate limit" or HTTP 429).
func isSecondaryRateLimit(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "secondary rate limit") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "429")
}

// upsertIssue finds an open issue with the given title and marker and updates it,
// or creates a new one if none exists. Returns (issueNumber, htmlURL, error).
func upsertIssue(ctx context.Context, client *github.Client, owner, repo, title, body string, labels []string) (int, string, error) {
	existing, err := findExistingIssue(ctx, client, owner, repo, title)
	if err != nil {
		return 0, "", err
	}

	if existing != nil {
		n := existing.GetNumber()
		_, _, err = client.Issues.Edit(ctx, owner, repo, n, &github.IssueRequest{Body: &body})
		if err != nil {
			return 0, "", fmt.Errorf("updating issue #%d in %s/%s: %w", n, owner, repo, err)
		}
		return n, existing.GetHTMLURL(), nil
	}

	if labels == nil {
		labels = []string{}
	}
	issue, _, err := client.Issues.Create(ctx, owner, repo, &github.IssueRequest{
		Title:  &title,
		Body:   &body,
		Labels: &labels,
	})
	if err != nil {
		return 0, "", fmt.Errorf("creating issue in %s/%s: %w", owner, repo, err)
	}
	return issue.GetNumber(), issue.GetHTMLURL(), nil
}

// findExistingIssue looks for an open issue containing the git-cascade marker.
func findExistingIssue(ctx context.Context, client *github.Client, owner, repo, title string) (*github.Issue, error) {
	opts := &github.IssueListByRepoOptions{
		State:       "open",
		ListOptions: github.ListOptions{PerPage: 100},
	}
	for {
		issues, resp, err := client.Issues.ListByRepo(ctx, owner, repo, opts)
		if err != nil {
			return nil, fmt.Errorf("listing issues for %s/%s: %w", owner, repo, err)
		}
		for _, issue := range issues {
			if issue.GetTitle() == title && strings.Contains(issue.GetBody(), gitCascadeMarker) {
				return issue, nil
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.ListOptions.Page = resp.NextPage
	}
	return nil, nil
}

// buildSummaryBody builds the issue body: scan metadata and a per-repo status
// table. Individual findings are in per-repo comments, not here. header
// overrides the default "# Compliance Report — {org}" heading when non-empty.
func buildSummaryBody(org string, byRepo map[string][]compliance.Result, ciURL string, scope config.Scope, header string) string {
	var sb strings.Builder
	sb.WriteString(gitCascadeMarker + "\n")
	if header == "" {
		header = fmt.Sprintf("# Compliance Report — %s", org)
	}
	fmt.Fprintf(&sb, "%s\n\n", header)

	updated := time.Now().UTC().Format(time.RFC3339)
	if ciURL != "" {
		fmt.Fprintf(&sb, "_Last updated: %s — [View CI run](%s)_\n\n", updated, ciURL)
	} else {
		fmt.Fprintf(&sb, "_Last updated: %s_\n\n", updated)
	}

	// Flatten to compute overall counts.
	var all []compliance.Result
	for _, r := range byRepo {
		all = append(all, r...)
	}
	passes, warnings, errors := countResults(all)
	totalRepos := len(byRepo)

	statusIcon := "✅"
	if errors > 0 {
		statusIcon = "❌"
	} else if warnings > 0 {
		statusIcon = "⚠️"
	}
	fmt.Fprintf(&sb, "%s **%d** repositor%s scanned — **%d** checks: **%d** passed · **%d** warnings · **%d** errors\n\n",
		statusIcon,
		totalRepos, map[bool]string{true: "y", false: "ies"}[totalRepos == 1],
		len(all), passes, warnings, errors)

	// Per-repo status table, sorted alphabetically.
	repos := make([]string, 0, len(byRepo))
	for r := range byRepo {
		repos = append(repos, r)
	}
	sort.Strings(repos)

	sb.WriteString("## Repository Status\n\n")
	sb.WriteString("| Repository | Visibility | Errors | Warnings | Status |\n")
	sb.WriteString("|------------|------------|-------:|---------:|--------|\n")
	for _, repoFull := range repos {
		repoResults := byRepo[repoFull]
		failures := filterFailed(repoResults)
		// filterFailed returns only error/warning failures; countResults on that
		// slice has passes=0 so warnings and errors are the useful return values.
		_, warnCount, errCount := countResults(failures)
		vis := "public"
		if len(repoResults) > 0 && repoResults[0].Private {
			vis = "private"
		}
		rowStatus := "✅ pass"
		if errCount > 0 {
			rowStatus = "❌ fail"
		} else if warnCount > 0 {
			rowStatus = "⚠️ warn"
		}
		fmt.Fprintf(&sb, "| `%s` | %s | %d | %d | %s |\n", repoFull, vis, errCount, warnCount, rowStatus)
	}

	sb.WriteString("\n> Each failing repository has a dedicated comment below with full details.\n\n")
	fmt.Fprintf(&sb, "---\n_Scope: %s_\n", scopeSummary(scope))
	return sb.String()
}

// buildFindingsDetail builds a per-repo findings breakdown for repos with
// failures, appended after the summary in the append-mode section comment
// (which — unlike mode=compliance — has no separate per-repo comments to
// hold this detail).
func buildFindingsDetail(byRepo map[string][]compliance.Result) string {
	repos := make([]string, 0, len(byRepo))
	for r := range byRepo {
		repos = append(repos, r)
	}
	sort.Strings(repos)

	var sb strings.Builder
	for _, repoFull := range repos {
		failures := filterFailed(byRepo[repoFull])
		if len(failures) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "\n## `%s` findings\n\n", repoFull)
		sb.WriteString("| Rule | Severity | Message |\n")
		sb.WriteString("|------|----------|---------|\n")
		for _, r := range failures {
			fmt.Fprintf(&sb, "| `%s` | %s | %s |\n", r.RuleID, r.Severity, escapeTableCell(r.Message))
		}
	}
	return sb.String()
}

// buildRepoCommentBody builds the per-repo detail comment body.
// The body is truncated to githubMaxBodyLen if it exceeds GitHub's limit.
func buildRepoCommentBody(repoFull string, failures []compliance.Result) string {
	var sb strings.Builder
	// Marker must be on the very first line so parseRepoMarker can find it.
	fmt.Fprintf(&sb, "%s%s -->\n", repoCommentPrefix, repoFull)

	visibility := visibilityLabel(failures[0].Private)
	fmt.Fprintf(&sb, "## `%s` %s\n\n", repoFull, visibility)
	fmt.Fprintf(&sb, "_Last updated: %s_\n\n", time.Now().UTC().Format(time.RFC3339))

	sb.WriteString("| Rule | Severity | Message |\n")
	sb.WriteString("|------|----------|---------|\n")
	for _, r := range failures {
		fmt.Fprintf(&sb, "| `%s` | %s | %s |\n", r.RuleID, r.Severity, escapeTableCell(r.Message))
	}

	body := sb.String()
	if len(body) > githubMaxBodyLen {
		// Truncate at a newline boundary and append a notice.
		cut := strings.LastIndex(body[:githubMaxBodyLen-100], "\n")
		if cut < 0 {
			cut = githubMaxBodyLen - 100
		}
		body = body[:cut] + "\n\n_… truncated — too many findings to display in a single comment._\n"
	}
	return body
}

// buildPerRepoBody builds the issue body for mode=repo (unchanged from before).
func buildPerRepoBody(repoFull string, failures []compliance.Result) string {
	var sb strings.Builder
	sb.WriteString(gitCascadeMarker + "\n")
	visibility := visibilityLabel(failures[0].Private)
	fmt.Fprintf(&sb, "# Compliance Findings — `%s` %s\n\n", repoFull, visibility)
	fmt.Fprintf(&sb, "_Last updated: %s_\n\n", time.Now().UTC().Format(time.RFC3339))
	sb.WriteString("| Rule | Severity | Message |\n")
	sb.WriteString("|------|----------|---------|\n")
	for _, r := range failures {
		fmt.Fprintf(&sb, "| `%s` | %s | %s |\n", r.RuleID, r.Severity, escapeTableCell(r.Message))
	}
	return sb.String()
}

// escapeTableCell escapes pipe characters in a Markdown table cell value.
func escapeTableCell(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

// visibilityLabel returns a Markdown badge for the repository visibility.
func visibilityLabel(private bool) string {
	if private {
		return "![private](https://img.shields.io/badge/visibility-private-orange)"
	}
	return "![public](https://img.shields.io/badge/visibility-public-blue)"
}

// groupByRepoSorted returns results grouped by repo full name as a map.
// It preserves all results (pass, fail, skip) so the summary can count accurately.
func groupByRepoSorted(results []compliance.Result) map[string][]compliance.Result {
	return groupByRepo(results)
}

// splitIntoBatches splits body into chunks each no longer than githubMaxBodyLen,
// cutting at newline boundaries to avoid splitting Markdown rows.
func splitIntoBatches(body string) []string {
	if len(body) <= githubMaxBodyLen {
		return []string{body}
	}
	var batches []string
	for len(body) > 0 {
		if len(body) <= githubMaxBodyLen {
			batches = append(batches, body)
			break
		}
		cut := githubMaxBodyLen
		if idx := strings.LastIndex(body[:cut], "\n"); idx > 0 {
			cut = idx + 1
		}
		batches = append(batches, body[:cut])
		body = body[cut:]
	}
	return batches
}

func countRepos(results []compliance.Result) int {
	seen := make(map[string]struct{})
	for _, r := range results {
		seen[r.Repo] = struct{}{}
	}
	return len(seen)
}

func filterFailed(results []compliance.Result) []compliance.Result {
	var out []compliance.Result
	for _, r := range results {
		if r.Status == compliance.StatusFail && (r.Severity == config.SeverityError || r.Severity == config.SeverityWarning) {
			out = append(out, r)
		}
	}
	return out
}

func splitRepo(fullName string) (owner, repo string, err error) {
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid repo %q: expected owner/repo format", fullName)
	}
	return parts[0], parts[1], nil
}
