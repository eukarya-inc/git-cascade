package notify

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	"github.com/google/go-github/v84/github"
)

const issueTitle = "[COMPLIANCE] non-compliant item findings"
const issueTitlePerRepo = "[COMPLIANCE] non-compliant item findings"
const gitCascadeMarker = "<!-- git-cascade -->"

// githubMaxBodyLen is GitHub's hard limit for issue bodies and comments.
const githubMaxBodyLen = 65536

// PostIssues creates or updates GitHub Issues with scan findings.
// mode="compliance": one consolidated issue in the compliance repo.
// mode="repo":       one issue per scanned repo that has failures, posted in that repo.
// ciURL is an optional link to the CI job run embedded in the issue body.
// Returns the HTML URL of the upserted issue for mode=compliance (empty string for mode=repo).
func PostIssues(ctx context.Context, client *github.Client, cfg config.IssuesConfig, org string, results []compliance.Result, ciURL string) (string, error) {
	switch cfg.Mode {
	case "repo":
		return "", postPerRepoIssues(ctx, client, cfg, results)
	case "compliance", "":
		return postConsolidatedIssue(ctx, client, cfg, org, results, ciURL)
	default:
		return "", fmt.Errorf("unknown issues mode %q (must be \"compliance\" or \"repo\")", cfg.Mode)
	}
}

// postConsolidatedIssue creates or updates a single issue in the compliance repo
// containing all findings grouped by repository.
// Returns the HTML URL of the created/updated issue.
func postConsolidatedIssue(ctx context.Context, client *github.Client, cfg config.IssuesConfig, org string, results []compliance.Result, ciURL string) (string, error) {
	repoRef := cfg.ComplianceRepo
	if repoRef == "" {
		repoRef = org + "/compliance"
	}
	owner, repo, err := splitRepo(repoRef)
	if err != nil {
		return "", err
	}

	body := buildConsolidatedBody(org, results, ciURL)
	url, err := upsertIssue(ctx, client, owner, repo, issueTitle, body, cfg.Labels)
	if err != nil {
		return "", err
	}
	return url, nil
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
		if _, err := upsertIssue(ctx, client, owner, repo, issueTitlePerRepo, body, cfg.Labels); err != nil { //nolint:errcheck
			return fmt.Errorf("posting issue to %s: %w", repoFull, err)
		}
	}
	return nil
}

// upsertIssue finds an open issue with the given title and marker and updates it,
// or creates a new one if none exists.
// If the body exceeds GitHub's limit it is split into batches: the first batch
// goes into the issue body, subsequent batches are posted as comments.
// Stale overflow comments from previous runs are deleted before new ones are added.
// Returns the HTML URL of the created/updated issue.
func upsertIssue(ctx context.Context, client *github.Client, owner, repo, title, body string, labels []string) (string, error) {
	batches := splitIntoBatches(body)

	existing, err := findExistingIssue(ctx, client, owner, repo, title)
	if err != nil {
		return "", err
	}

	var issueNumber int
	var htmlURL string

	if existing != nil {
		issueNumber = existing.GetNumber()
		htmlURL = existing.GetHTMLURL()
		_, _, err = client.Issues.Edit(ctx, owner, repo, issueNumber, &github.IssueRequest{
			Body: &batches[0],
		})
		if err != nil {
			return "", fmt.Errorf("updating issue #%d in %s/%s: %w", issueNumber, owner, repo, err)
		}
		// Delete previous overflow comments so we don't accumulate stale ones.
		if err := deleteOverflowComments(ctx, client, owner, repo, issueNumber); err != nil {
			return "", err
		}
	} else {
		if labels == nil {
			labels = []string{}
		}
		issue, _, err := client.Issues.Create(ctx, owner, repo, &github.IssueRequest{
			Title:  &title,
			Body:   &batches[0],
			Labels: &labels,
		})
		if err != nil {
			return "", fmt.Errorf("creating issue in %s/%s: %w", owner, repo, err)
		}
		issueNumber = issue.GetNumber()
		htmlURL = issue.GetHTMLURL()
	}

	// Post overflow batches as comments.
	for i, batch := range batches[1:] {
		comment := fmt.Sprintf("<!-- git-cascade-overflow -->\n_Continued (part %d/%d)_\n\n%s", i+2, len(batches), batch)
		if _, _, err := client.Issues.CreateComment(ctx, owner, repo, issueNumber, &github.IssueComment{
			Body: &comment,
		}); err != nil {
			return "", fmt.Errorf("posting overflow comment on #%d in %s/%s: %w", issueNumber, owner, repo, err)
		}
	}

	return htmlURL, nil
}

// deleteOverflowComments removes comments previously posted by git-cascade as overflow batches.
func deleteOverflowComments(ctx context.Context, client *github.Client, owner, repo string, issueNumber int) error {
	opts := &github.IssueListCommentsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}
	for {
		comments, resp, err := client.Issues.ListComments(ctx, owner, repo, issueNumber, opts)
		if err != nil {
			return fmt.Errorf("listing comments on #%d in %s/%s: %w", issueNumber, owner, repo, err)
		}
		for _, c := range comments {
			if strings.Contains(c.GetBody(), "<!-- git-cascade-overflow -->") {
				if _, err := client.Issues.DeleteComment(ctx, owner, repo, c.GetID()); err != nil {
					return fmt.Errorf("deleting comment %d: %w", c.GetID(), err)
				}
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.ListOptions.Page = resp.NextPage
	}
	return nil
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

func buildConsolidatedBody(org string, results []compliance.Result, ciURL string) string {
	var sb strings.Builder
	sb.WriteString(gitCascadeMarker + "\n")
	fmt.Fprintf(&sb, "# Compliance Findings — %s\n\n", org)
	updated := time.Now().UTC().Format(time.RFC3339)
	if ciURL != "" {
		fmt.Fprintf(&sb, "_Last updated: %s — [View CI run](%s)_\n\n", updated, ciURL)
	} else {
		fmt.Fprintf(&sb, "_Last updated: %s_\n\n", updated)
	}

	byRepo := groupByRepo(results)
	hasAnyFailure := false
	for repo, repoResults := range byRepo {
		failures := filterFailed(repoResults)
		if len(failures) == 0 {
			continue
		}
		hasAnyFailure = true
		visibility := visibilityLabel(repoResults[0].Private)
		fmt.Fprintf(&sb, "## `%s` %s\n\n", repo, visibility)
		sb.WriteString("| Rule | Severity | Message |\n")
		sb.WriteString("|------|----------|---------|\n")
		for _, r := range failures {
			fmt.Fprintf(&sb, "| `%s` | %s | %s |\n", r.RuleID, r.Severity, r.Message)
		}
		sb.WriteString("\n")
	}

	if !hasAnyFailure {
		sb.WriteString("✅ All compliance checks passed.\n")
	}

	passes, warnings, errors := countResults(results)
	fmt.Fprintf(&sb, "\n---\n_Scanned %d repositories / %d checks — %d passed, %d warnings, %d errors_\n",
		countRepos(results), len(results), passes, warnings, errors)
	return sb.String()
}

func buildPerRepoBody(repoFull string, failures []compliance.Result) string {
	var sb strings.Builder
	sb.WriteString(gitCascadeMarker + "\n")
	visibility := visibilityLabel(failures[0].Private)
	fmt.Fprintf(&sb, "# Compliance Findings — `%s` %s\n\n", repoFull, visibility)
	fmt.Fprintf(&sb, "_Last updated: %s_\n\n", time.Now().UTC().Format(time.RFC3339))
	sb.WriteString("| Rule | Severity | Message |\n")
	sb.WriteString("|------|----------|---------|\n")
	for _, r := range failures {
		fmt.Fprintf(&sb, "| `%s` | %s | %s |\n", r.RuleID, r.Severity, r.Message)
	}
	return sb.String()
}

// visibilityLabel returns a Markdown badge for the repository visibility.
func visibilityLabel(private bool) string {
	if private {
		return "![private](https://img.shields.io/badge/visibility-private-orange)"
	}
	return "![public](https://img.shields.io/badge/visibility-public-blue)"
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
