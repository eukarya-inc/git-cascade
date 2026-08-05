package checks

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/google/go-github/v90/github"
)

// shaRef matches a full 40-char hex SHA.
var shaRef = regexp.MustCompile(`^[0-9a-f]{40}$`)

// usesPattern matches `uses: owner/repo@ref` lines in workflow YAML.
// It captures the ref portion after @.
var usesPattern = regexp.MustCompile(`uses:\s*['"]?([^@'"]+)@([^'"#\s]+)`)

// UnpinnedAction is a single `uses: owner/repo@ref` occurrence found by
// FindUnpinnedActions whose ref is not a full commit SHA.
type UnpinnedAction struct {
	Line   int    // 0-based index into the content's lines
	Action string // e.g. "actions/checkout"
	Ref    string // e.g. "v4"
}

// FindUnpinnedActions scans a single workflow file's content for `uses:`
// references that are not pinned to a 40-character SHA, skipping local
// (./...) and docker:// actions and lines suppressed with a
// git-cascade:allow comment. Exported so the actions-pinned remediator can
// reuse the exact same detection logic as this checker.
func FindUnpinnedActions(content string) []UnpinnedAction {
	lines := strings.Split(content, "\n")
	var found []UnpinnedAction
	for i, line := range lines {
		m := usesPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		action := m[1]
		actionRef := m[2]

		if strings.HasPrefix(action, ".") {
			continue
		}
		if strings.HasPrefix(action, "docker://") {
			continue
		}
		if shaRef.MatchString(actionRef) {
			continue
		}
		if hasAllowComment(lines, i) {
			continue
		}
		found = append(found, UnpinnedAction{Line: i, Action: action, Ref: actionRef})
	}
	return found
}

type actionsPinnedChecker struct{}

func (c *actionsPinnedChecker) ID() string { return "actions-pinned" }

func (c *actionsPinnedChecker) Check(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule) (*compliance.Result, error) {
	ref := repo.DefaultBranch

	// List workflow files
	dirContent, err := gh.CachedListDirectoryContents(ctx, client, repo.Owner, repo.Name, ".github/workflows", ref)
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

		content, err := gh.CachedFetchFileContent(ctx, client, repo.Owner, repo.Name, entry.GetPath(), ref)
		if err != nil {
			return nil, err
		}
		if content == nil {
			continue
		}

		for _, v := range FindUnpinnedActions(string(content)) {
			violations = append(violations, fmt.Sprintf("%s: %s@%s", name, v.Action, v.Ref))
		}
	}

	if len(violations) > 0 {
		msg := fmt.Sprintf("%d action(s) not pinned to SHA: %s", len(violations), strings.Join(violations, "; "))
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusFail,
			Severity: rule.Severity,
			Message:  msg,
		}, nil
	}

	return &compliance.Result{
		RuleID:   rule.ID,
		RuleName: rule.Name,
		Repo:     repo.FullName,
		Status:   compliance.StatusPass,
		Severity: rule.Severity,
		Message:  "all actions pinned to SHA",
	}, nil
}

func init() {
	compliance.Register(&actionsPinnedChecker{})
}
