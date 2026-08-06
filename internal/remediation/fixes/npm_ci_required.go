package fixes

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/compliance/checks"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/eukarya-inc/git-cascade/internal/remediation"
	"github.com/google/go-github/v90/github"
)

// npmInstallToken matches the `npm install` command token so it can be
// swapped for `npm ci` in place, preserving any leading whitespace/`run:` prefix.
var npmInstallToken = regexp.MustCompile(`\bnpm\s+install\b`)

type npmCiRequiredFixer struct{}

func (f *npmCiRequiredFixer) ID() string { return "npm-ci-required" }

func (f *npmCiRequiredFixer) Remediate(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule, result compliance.Result) (*remediation.Fix, error) {
	ref := repo.DefaultBranch

	dirContent, err := gh.CachedListDirectoryContents(ctx, client, repo.Owner, repo.Name, ".github/workflows", ref)
	if err != nil {
		return nil, err
	}

	var files []remediation.FileChange
	var fixedDescs []string
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

		violations := checks.FindNodeInstallViolations(string(content))
		if len(violations) == 0 {
			continue
		}

		lines := strings.Split(string(content), "\n")
		var fixedAny bool
		for _, v := range violations {
			newLine, ok := lockNodeInstallLine(lines[v.Line], v.Kind)
			if !ok {
				continue
			}
			fixedDescs = append(fixedDescs, fmt.Sprintf("%s: `%s` → `%s`", name, strings.TrimSpace(lines[v.Line]), strings.TrimSpace(newLine)))
			lines[v.Line] = newLine
			fixedAny = true
		}
		if !fixedAny {
			continue
		}
		files = append(files, remediation.FileChange{Path: entry.GetPath(), Content: []byte(strings.Join(lines, "\n"))})
	}

	if len(files) == 0 {
		return nil, nil
	}

	return &remediation.Fix{
		Files:         files,
		CommitMessage: "fix(ci): use locked install commands in CI workflows",
		PRTitle:       "fix(npm-ci-required): use locked Node.js install commands",
		PRBody: fmt.Sprintf(
			"Automated fix for the `npm-ci-required` compliance rule.\n\nLocked install commands:\n- %s\n\n_Opened automatically by git-cascade auto-remediation._",
			strings.Join(fixedDescs, "\n- "),
		),
	}, nil
}

// lockNodeInstallLine rewrites a single non-locked install line to its locked
// equivalent. Returns (line, false) when the line's shape isn't one this
// fixer can rewrite safely — a compound command (&&, ;) where blindly
// swapping/appending could change an unrelated command, or an `npm install`
// invocation that isn't a bare restore (e.g. `npm install some-pkg`, which
// `npm ci` cannot express) is left untouched rather than guessed at.
func lockNodeInstallLine(line, kind string) (string, bool) {
	if strings.Contains(line, "&&") || strings.Contains(line, ";") {
		return line, false
	}
	switch kind {
	case "npm":
		if !isBareNpmInstall(line) {
			return line, false
		}
		return npmInstallToken.ReplaceAllString(line, "npm ci"), true
	case "pnpm":
		return line + " --frozen-lockfile", true
	case "yarn":
		return line + " --immutable", true
	default:
		return line, false
	}
}

// isBareNpmInstall reports whether line's `npm install` invocation has no
// trailing arguments other than flags (e.g. `--no-audit` is fine, a bare
// package name like `left-pad` is not — that installs a dependency, which
// `npm ci` cannot do).
func isBareNpmInstall(line string) bool {
	loc := npmInstallToken.FindStringIndex(line)
	if loc == nil {
		return false
	}
	rest := strings.TrimSpace(line[loc[1]:])
	if rest == "" {
		return true
	}
	for _, tok := range strings.Fields(rest) {
		if !strings.HasPrefix(tok, "-") {
			return false
		}
	}
	return true
}

func init() {
	remediation.Register(&npmCiRequiredFixer{})
}
