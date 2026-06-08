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

// npmCiOrInstallPattern matches `npm ci` or `npm install` (locked or not). It is
// used to decide whether the check applies to a workflow file at all.
var npmCiOrInstallPattern = regexp.MustCompile(`\bnpm\s+(?:ci|install)(?:\s|$)`)

// pnpmInstallPattern matches `pnpm install` (bare, triggering a violation check).
// pnpm add / pnpm i are different commands; only `pnpm install` is checked.
var pnpmInstallPattern = regexp.MustCompile(`\bpnpm\s+install(?:\s|$)`)
var pnpmFrozenPattern = regexp.MustCompile(`\bpnpm\s+install\b.*--frozen-lockfile`)

// yarnInstallPattern matches an install-performing yarn invocation: an explicit
// `yarn install ...`, a bare `yarn` with no subcommand (which performs an install
// under Yarn Berry), or `yarn --<flag>` such as `yarn --immutable`. yarn add /
// yarn run / yarn build etc. are intentionally excluded. Informational
// invocations such as `yarn --version` also match here but are filtered out by
// yarnInfoFlagPattern below, since they perform no install.
var yarnInstallPattern = regexp.MustCompile(`\byarn(?:\s+install\b|\s+--|\s*$)`)

// yarnInfoFlagPattern matches a yarn invocation whose argument only prints
// information (version or help). These perform no install and must not be
// treated as one — `yarn --version` is a common CI diagnostic step that would
// otherwise be misreported as an unlocked install.
var yarnInfoFlagPattern = regexp.MustCompile(`\byarn\s+(?:--version|-v|--help|-h)\b`)

// yarnLockedPattern matches yarn (install) with --frozen-lockfile or --immutable
// (--immutable is the Yarn Berry / v2+ equivalent).
var yarnLockedPattern = regexp.MustCompile(`\byarn\b.*(?:--frozen-lockfile|--immutable)`)

type nodejsInstallLockChecker struct{}

func (c *nodejsInstallLockChecker) ID() string { return "npm-ci-required" }

func (c *nodejsInstallLockChecker) Check(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule) (*compliance.Result, error) {
	ref := repo.DefaultBranch

	dirContent, err := gh.CachedListDirectoryContents(ctx, client, repo.Owner, repo.Name, ".github/workflows", ref)
	if err != nil {
		return nil, fmt.Errorf("listing workflows for %s: %w", repo.FullName, err)
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
	hasInstallCommands := false
	for _, entry := range dirContent {
		name := entry.GetName()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}

		content, err := gh.CachedFetchFileContent(ctx, client, repo.Owner, repo.Name, entry.GetPath(), ref)
		if err != nil {
			return nil, err
		}
		if content == nil {
			continue
		}

		reason, sawInstall := scanWorkflow(string(content))
		if sawInstall {
			hasInstallCommands = true
		}
		if reason != "" {
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

// scanWorkflow inspects a workflow file's content line by line. It returns a
// short reason string for the first non-locked install command found (empty if
// none), and whether any Node.js install command — locked or not — was seen.
// The latter determines whether this check applies to the repository at all, so
// a file whose only yarn usage is `yarn --version` reports sawInstall=false.
func scanWorkflow(content string) (reason string, sawInstall bool) {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if !isNodeInstallLine(line) {
			continue
		}
		sawInstall = true
		if hasAllowComment(lines, i) {
			continue
		}
		if reason != "" {
			continue
		}
		switch {
		case hasNpmInstallViolation(line):
			reason = "npm install"
		case hasPnpmInstallViolation(line):
			reason = "pnpm install without --frozen-lockfile"
		case hasYarnInstallViolation(line):
			reason = "yarn install without --frozen-lockfile/--immutable"
		}
	}
	return reason, sawInstall
}

// installViolation returns a short reason string for the first non-locked
// install command in content, or an empty string if none. It is a thin wrapper
// over scanWorkflow that discards the applicability flag.
func installViolation(content string) string {
	reason, _ := scanWorkflow(content)
	return reason
}

// isNodeInstallLine reports whether a single line contains any npm/pnpm/yarn
// install invocation (locked or not, compliant or not).
func isNodeInstallLine(line string) bool {
	return npmCiOrInstallPattern.MatchString(line) ||
		pnpmInstallPattern.MatchString(line) ||
		isYarnInstallLine(line)
}

// isYarnInstallLine reports whether a single line is an install-performing yarn
// invocation, excluding informational ones such as `yarn --version`.
func isYarnInstallLine(line string) bool {
	return yarnInstallPattern.MatchString(line) && !yarnInfoFlagPattern.MatchString(line)
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
// or `yarn install` (but not an informational `yarn --version`) without
// --frozen-lockfile or --immutable.
func hasYarnInstallViolation(line string) bool {
	return isYarnInstallLine(line) && !yarnLockedPattern.MatchString(line)
}

func init() {
	compliance.Register(&nodejsInstallLockChecker{})
}
