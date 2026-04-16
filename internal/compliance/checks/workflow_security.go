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

// — harden-runner checker ———————————————————————————————————————————————————

type hardenRunnerChecker struct{}

func (c *hardenRunnerChecker) ID() string { return "harden-runner-required" }

func (c *hardenRunnerChecker) Check(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule) (*compliance.Result, error) {
	// Only applies to public repositories.
	if repo.Private {
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusSkip,
			Severity: rule.Severity,
			Message:  "skipped for private repository",
		}, nil
	}

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

		if missing := jobsMissingHardenRunner(string(content)); len(missing) > 0 {
			violations = append(violations, fmt.Sprintf("%s (jobs: %s)", name, strings.Join(missing, ", ")))
		}
	}

	if len(violations) > 0 {
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusFail,
			Severity: rule.Severity,
			Message:  fmt.Sprintf("jobs missing step-security/harden-runner as first step: %s", strings.Join(violations, "; ")),
		}, nil
	}

	return &compliance.Result{
		RuleID:   rule.ID,
		RuleName: rule.Name,
		Repo:     repo.FullName,
		Status:   compliance.StatusPass,
		Severity: rule.Severity,
		Message:  "all jobs use step-security/harden-runner as first step",
	}, nil
}

// jobsMissingHardenRunner parses workflow YAML line-by-line and returns the
// names of jobs whose first step does not use step-security/harden-runner.
//
// Parsing strategy (indentation-based, no full YAML parser):
//
//  1. Locate the top-level `jobs:` key.
//  2. Each 2-space-indented key directly under `jobs:` is a job name.
//  3. Within a job, locate its `steps:` key.
//  4. The first list item (`- `) under `steps:` must contain a `uses:` field
//     that starts with `step-security/harden-runner`.  We look ahead within
//     that item's lines until we either find `uses:` or reach the next item /
//     next job / end of file.
func jobsMissingHardenRunner(content string) []string {
	lines := strings.Split(content, "\n")

	inJobs := false
	currentJob := ""
	// inSteps is true while we are still inside the `steps:` block of a job
	// (set to false once we pass the first step, so continuation lines stop
	// being examined).
	inSteps := false
	// hadSteps is true once we have seen a `steps:` key for the current job.
	hadSteps := false
	firstStepChecked := false
	firstStepHardened := false

	var missing []string

	finishJob := func() {
		if currentJob == "" {
			return
		}
		if hadSteps && firstStepChecked && !firstStepHardened {
			missing = append(missing, currentJob)
		}
	}

	for _, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		indent := indentOf(raw)

		if !inJobs {
			if indent == 0 && trimmed == "jobs:" {
				inJobs = true
			}
			continue
		}

		// A new top-level key ends the jobs block.
		if indent == 0 && strings.HasSuffix(trimmed, ":") && trimmed != "jobs:" {
			finishJob()
			currentJob = ""
			break
		}

		// 2-space-indented key directly under `jobs:` is a job ID.
		if indent == 2 && strings.HasSuffix(trimmed, ":") && !strings.HasPrefix(trimmed, "-") {
			finishJob()
			currentJob = strings.TrimSuffix(trimmed, ":")
			inSteps = false
			hadSteps = false
			firstStepChecked = false
			firstStepHardened = false
			continue
		}

		if currentJob == "" {
			continue
		}

		if indent == 4 && trimmed == "steps:" {
			inSteps = true
			hadSteps = true
			firstStepChecked = false
			firstStepHardened = false
			continue
		}

		if !inSteps {
			continue
		}

		// Each step starts with a `- ` list item marker at indent 6.
		if indent == 6 && strings.HasPrefix(trimmed, "- ") {
			if !firstStepChecked {
				firstStepChecked = true
				rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
				if strings.HasPrefix(rest, "uses:") {
					value := strings.TrimSpace(strings.TrimPrefix(rest, "uses:"))
					firstStepHardened = isHardenRunner(value)
				}
				// uses: may appear on a continuation line at indent 8 — handled below.
			} else {
				// Second step: no need to scan further for this job.
				inSteps = false
			}
			continue
		}

		// Continuation lines of the first step at indent 8.
		if inSteps && firstStepChecked && !firstStepHardened && indent == 8 {
			if strings.HasPrefix(trimmed, "uses:") {
				value := strings.TrimSpace(strings.TrimPrefix(trimmed, "uses:"))
				firstStepHardened = isHardenRunner(value)
			}
		}
	}

	finishJob()
	return missing
}

// isHardenRunner reports whether a `uses:` value refers to
// step-security/harden-runner (any version / SHA).
func isHardenRunner(value string) bool {
	if idx := strings.Index(value, " #"); idx != -1 {
		value = strings.TrimSpace(value[:idx])
	}
	return strings.HasPrefix(value, "step-security/harden-runner@")
}

// indentOf returns the number of leading spaces in s.
func indentOf(s string) int {
	return len(s) - len(strings.TrimLeft(s, " "))
}

func init() {
	compliance.Register(&hardenRunnerChecker{})
}
