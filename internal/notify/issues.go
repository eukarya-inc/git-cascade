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

const issueTitle = "git-cascade compliance findings"
const issueTitlePerRepo = "git-cascade compliance findings"
const gitCascadeMarker = "<!-- git-cascade -->"

// PostIssues creates or updates GitHub Issues with scan findings.
// mode="compliance": one consolidated issue in the compliance repo.
// mode="repo":       one issue per scanned repo that has failures, posted in that repo.
func PostIssues(ctx context.Context, client *github.Client, cfg config.IssuesConfig, org string, results []compliance.Result) error {
	switch cfg.Mode {
	case "repo":
		return postPerRepoIssues(ctx, client, cfg, results)
	case "compliance", "":
		return postConsolidatedIssue(ctx, client, cfg, org, results)
	default:
		return fmt.Errorf("unknown issues mode %q (must be \"compliance\" or \"repo\")", cfg.Mode)
	}
}

// postConsolidatedIssue creates or updates a single issue in the compliance repo
// containing all findings grouped by repository.
func postConsolidatedIssue(ctx context.Context, client *github.Client, cfg config.IssuesConfig, org string, results []compliance.Result) error {
	repoRef := cfg.ComplianceRepo
	if repoRef == "" {
		repoRef = org + "/compliance"
	}
	owner, repo, err := splitRepo(repoRef)
	if err != nil {
		return err
	}

	body := buildConsolidatedBody(org, results)
	return upsertIssue(ctx, client, owner, repo, issueTitle, body, cfg.Labels)
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
		if err := upsertIssue(ctx, client, owner, repo, issueTitlePerRepo, body, cfg.Labels); err != nil {
			return fmt.Errorf("posting issue to %s: %w", repoFull, err)
		}
	}
	return nil
}

// upsertIssue finds an open issue with the given title and marker and updates it,
// or creates a new one if none exists.
func upsertIssue(ctx context.Context, client *github.Client, owner, repo, title, body string, labels []string) error {
	existing, err := findExistingIssue(ctx, client, owner, repo, title)
	if err != nil {
		return err
	}

	ghLabels := make([]*github.Label, len(labels))
	for i, l := range labels {
		name := l
		ghLabels[i] = &github.Label{Name: &name}
	}

	if existing != nil {
		_, _, err = client.Issues.Edit(ctx, owner, repo, existing.GetNumber(), &github.IssueRequest{
			Body: &body,
		})
		if err != nil {
			return fmt.Errorf("updating issue #%d in %s/%s: %w", existing.GetNumber(), owner, repo, err)
		}
		return nil
	}

	labelNames := labels
	_, _, err = client.Issues.Create(ctx, owner, repo, &github.IssueRequest{
		Title:  &title,
		Body:   &body,
		Labels: &labelNames,
	})
	if err != nil {
		return fmt.Errorf("creating issue in %s/%s: %w", owner, repo, err)
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

func buildConsolidatedBody(org string, results []compliance.Result) string {
	var sb strings.Builder
	sb.WriteString(gitCascadeMarker + "\n")
	fmt.Fprintf(&sb, "# Compliance Findings — %s\n\n", org)
	fmt.Fprintf(&sb, "_Last updated: %s_\n\n", time.Now().UTC().Format(time.RFC3339))

	byRepo := groupByRepo(results)
	hasAnyFailure := false
	for repo, repoResults := range byRepo {
		failures := filterFailed(repoResults)
		if len(failures) == 0 {
			continue
		}
		hasAnyFailure = true
		fmt.Fprintf(&sb, "## `%s`\n\n", repo)
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
	fmt.Fprintf(&sb, "\n---\n_Total: %d checks — %d passed, %d warnings, %d errors_\n",
		len(results), passes, warnings, errors)
	return sb.String()
}

func buildPerRepoBody(repoFull string, failures []compliance.Result) string {
	var sb strings.Builder
	sb.WriteString(gitCascadeMarker + "\n")
	fmt.Fprintf(&sb, "# Compliance Findings — `%s`\n\n", repoFull)
	fmt.Fprintf(&sb, "_Last updated: %s_\n\n", time.Now().UTC().Format(time.RFC3339))
	sb.WriteString("| Rule | Severity | Message |\n")
	sb.WriteString("|------|----------|---------|\n")
	for _, r := range failures {
		fmt.Fprintf(&sb, "| `%s` | %s | %s |\n", r.RuleID, r.Severity, r.Message)
	}
	return sb.String()
}

func filterFailed(results []compliance.Result) []compliance.Result {
	var out []compliance.Result
	for _, r := range results {
		if r.Status == compliance.StatusFail {
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
