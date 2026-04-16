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

// npmInstallPattern matches `npm install` but not `pnpm install` (word boundary
// before `npm` prevents matching inside `pnpm`).
var npmInstallPattern = regexp.MustCompile(`\bnpm\s+install(?:\s|$)`)

// npmGlobalInstallPattern matches `npm install -g` (which is acceptable).
var npmGlobalInstallPattern = regexp.MustCompile(`\bnpm\s+install\s+.*-g\b`)

// pnpmInstallPattern matches `pnpm install` (bare, triggering a violation check).
// pnpm add / pnpm i are different commands; only `pnpm install` is checked.
var pnpmInstallPattern = regexp.MustCompile(`\bpnpm\s+install(?:\s|$)`)
var pnpmFrozenPattern = regexp.MustCompile(`\bpnpm\s+install\b.*--frozen-lockfile`)

// yarnInstallPattern matches `yarn install` or a bare `yarn` with no subcommand.
// yarn add / yarn run / yarn build etc. are intentionally excluded.
var yarnInstallPattern = regexp.MustCompile(`\byarn(?:\s+install)?(?:\s+--|(?:\s*$))`)

// yarnLockedPattern matches yarn (install) with --frozen-lockfile or --immutable
// (--immutable is the Yarn Berry / v2+ equivalent).
var yarnLockedPattern = regexp.MustCompile(`\byarn\b.*(?:--frozen-lockfile|--immutable)`)

// anyNodeInstallPattern detects any npm/pnpm/yarn install invocation (locked or
// not) and is used to decide whether the check is applicable to a workflow file.
var anyNodeInstallPattern = regexp.MustCompile(`\b(?:npm\s+(?:ci|install)|pnpm\s+install|yarn(?:\s+install)?(?:\s+--|(?:\s*$)))`)

type nodejsInstallLockChecker struct{}

func (c *nodejsInstallLockChecker) ID() string { return "npm-ci-required" }

func (c *nodejsInstallLockChecker) Check(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule) (*compliance.Result, error) {
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
	hasInstallCommands := false
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

		text := string(content)
		if anyNodeInstallPattern.MatchString(text) {
			hasInstallCommands = true
		}
		if reason := installViolation(text); reason != "" {
			violations = append(violations, fmt.Sprintf("%s (%s)", name, reason))
		}
	}

	if !hasInstallCommands {
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusSkip,
			Severity: rule.Severity,
			Message:  "no Node.js install commands found in CI workflows",
		}, nil
	}

	if len(violations) > 0 {
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusFail,
			Severity: rule.Severity,
			Message:  fmt.Sprintf("CI workflows use non-locked install commands: %s", strings.Join(violations, ", ")),
		}, nil
	}

	return &compliance.Result{
		RuleID:   rule.ID,
		RuleName: rule.Name,
		Repo:     repo.FullName,
		Status:   compliance.StatusPass,
		Severity: rule.Severity,
		Message:  "CI workflows use locked install commands (npm ci / pnpm install --frozen-lockfile / yarn --immutable)",
	}, nil
}

// installViolation returns a short reason string if the workflow content
// contains an install command that does not enforce the lockfile, or an empty
// string if everything looks fine.
func installViolation(content string) string {
	for line := range strings.SplitSeq(content, "\n") {
		if hasNpmInstallViolation(line) {
			return "npm install"
		}
		if hasPnpmInstallViolation(line) {
			return "pnpm install without --frozen-lockfile"
		}
		if hasYarnInstallViolation(line) {
			return "yarn install without --frozen-lockfile/--immutable"
		}
	}
	return ""
}

// hasNpmInstallViolation checks whether a single line contains `npm install`
// that is not a global install (`npm install -g`).
func hasNpmInstallViolation(line string) bool {
	return npmInstallPattern.MatchString(line) && !npmGlobalInstallPattern.MatchString(line)
}

// hasPnpmInstallViolation checks whether a single line contains `pnpm install`
// without the --frozen-lockfile flag.
func hasPnpmInstallViolation(line string) bool {
	return pnpmInstallPattern.MatchString(line) && !pnpmFrozenPattern.MatchString(line)
}

// hasYarnInstallViolation checks whether a single line contains a bare `yarn`
// or `yarn install` without --frozen-lockfile or --immutable.
func hasYarnInstallViolation(line string) bool {
	return yarnInstallPattern.MatchString(line) && !yarnLockedPattern.MatchString(line)
}

func init() {
	compliance.Register(&nodejsInstallLockChecker{})
}
