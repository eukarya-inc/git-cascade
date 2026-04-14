package checks

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/google/go-github/v84/github"
)

// npmInstallPattern matches `npm install` lines in workflow files.
var npmInstallPattern = regexp.MustCompile(`npm\s+install(?:\s|$)`)

// npmGlobalInstallPattern matches `npm install -g` (which is acceptable).
var npmGlobalInstallPattern = regexp.MustCompile(`npm\s+install\s+.*-g\b`)

type npmCIChecker struct{}

func (c *npmCIChecker) ID() string { return "npm-ci-required" }

func (c *npmCIChecker) Check(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule) (*compliance.Result, error) {
	ref := repo.DefaultBranch

	_, dirContent, resp, err := client.Repositories.GetContents(ctx, repo.Owner, repo.Name, ".github/workflows", &github.RepositoryContentGetOptions{Ref: ref})
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return &compliance.Result{
				RuleID:   rule.ID,
				RuleName: rule.Name,
				Repo:     repo.FullName,
				Status:   compliance.StatusSkip,
				Severity: rule.Severity,
				Message:  "no .github/workflows directory",
			}, nil
		}
		return nil, fmt.Errorf("listing workflows for %s: %w", repo.FullName, err)
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

		if hasNpmInstallViolation(string(content)) {
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
			Message:  fmt.Sprintf("CI workflows use 'npm install' instead of 'npm ci': %s", strings.Join(violations, ", ")),
		}, nil
	}

	return &compliance.Result{
		RuleID:   rule.ID,
		RuleName: rule.Name,
		Repo:     repo.FullName,
		Status:   compliance.StatusPass,
		Severity: rule.Severity,
		Message:  "CI workflows use npm ci (or no npm usage found)",
	}, nil
}

// hasNpmInstallViolation checks whether a workflow file contains `npm install`
// that is not a global install (`npm install -g`). It checks line by line so
// that a file with both global and local installs is correctly flagged.
func hasNpmInstallViolation(content string) bool {
	for line := range strings.SplitSeq(content, "\n") {
		if npmInstallPattern.MatchString(line) && !npmGlobalInstallPattern.MatchString(line) {
			return true
		}
	}
	return false
}

func init() {
	compliance.Register(&npmCIChecker{})
}
