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

// — pull_request_target checker —————————————————————————————————————————————

type pullRequestTargetChecker struct{}

func (c *pullRequestTargetChecker) ID() string { return "no-pull-request-target" }

func (c *pullRequestTargetChecker) Check(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule) (*compliance.Result, error) {
	ref := repo.DefaultBranch

	dirContent, err := gh.ListDirectoryContents(ctx, client, repo.Owner, repo.Name, ".github/workflows", ref)
	if err != nil {
		return nil, err
	}
	if dirContent == nil {
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusSkip,
			Severity: rule.Severity,
			Message:  "no .github/workflows directory",
		}, nil
	}

	var violations []string
	for _, entry := range dirContent {
		name := entry.GetName()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}
		content, err := gh.FetchFileContent(ctx, client, repo.Owner, repo.Name, entry.GetPath(), ref)
		if err != nil {
			return nil, err
		}
		if content == nil {
			continue
		}
		if hasPullRequestTarget(string(content)) {
			violations = append(violations, name)
		}
	}

	if len(violations) > 0 {
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusFail,
			Severity: rule.Severity,
			Message:  fmt.Sprintf("pull_request_target event used in: %s", strings.Join(violations, ", ")),
		}, nil
	}

	return &compliance.Result{
		RuleID:   rule.ID,
		RuleName: rule.Name,
		Repo:     repo.FullName,
		Status:   compliance.StatusPass,
		Severity: rule.Severity,
		Message:  "no pull_request_target event usage",
	}, nil
}

// hasPullRequestTarget reports whether workflow YAML content uses the
// pull_request_target event trigger.
func hasPullRequestTarget(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		// Map key:  "pull_request_target:" or "pull_request_target: ..."
		if trimmed == "pull_request_target:" || strings.HasPrefix(trimmed, "pull_request_target:") {
			return true
		}
		// List item: "- pull_request_target"
		if trimmed == "- pull_request_target" {
			return true
		}
	}
	return false
}

func init() {
	compliance.Register(&pullRequestTargetChecker{})
}

// — secrets: inherit checker ————————————————————————————————————————————————

type secretsInheritChecker struct{}

func (c *secretsInheritChecker) ID() string { return "no-secrets-inherit" }

func (c *secretsInheritChecker) Check(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule) (*compliance.Result, error) {
	ref := repo.DefaultBranch

	dirContent, err := gh.ListDirectoryContents(ctx, client, repo.Owner, repo.Name, ".github/workflows", ref)
	if err != nil {
		return nil, err
	}
	if dirContent == nil {
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusSkip,
			Severity: rule.Severity,
			Message:  "no .github/workflows directory",
		}, nil
	}

	var violations []string
	for _, entry := range dirContent {
		name := entry.GetName()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}
		content, err := gh.FetchFileContent(ctx, client, repo.Owner, repo.Name, entry.GetPath(), ref)
		if err != nil {
			return nil, err
		}
		if content == nil {
			continue
		}
		if hasSecretsInherit(string(content)) {
			violations = append(violations, name)
		}
	}

	if len(violations) > 0 {
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusFail,
			Severity: rule.Severity,
			Message:  fmt.Sprintf("secrets: inherit used in: %s", strings.Join(violations, ", ")),
		}, nil
	}

	return &compliance.Result{
		RuleID:   rule.ID,
		RuleName: rule.Name,
		Repo:     repo.FullName,
		Status:   compliance.StatusPass,
		Severity: rule.Severity,
		Message:  "no secrets: inherit usage",
	}, nil
}

// hasSecretsInherit reports whether workflow YAML content contains a
// `secrets: inherit` directive in a job's `uses:` call.
func hasSecretsInherit(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "secrets: inherit" {
			return true
		}
	}
	return false
}

func init() {
	compliance.Register(&secretsInheritChecker{})
}
