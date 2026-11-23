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
	"gopkg.in/yaml.v3"
)

// ActionsLockPath is where `gh actions-lock` writes its lockfile
// (https://gh.io/actions-lockfile).
const ActionsLockPath = ".github/workflows/actions.lock"

// ActionsLock is the subset of a `gh actions-lock` lockfile needed to decide
// whether a `uses: owner/repo@ref` is pinned: the workflow must list the
// reference and the reference must resolve to a commit.
type ActionsLock struct {
	Workflows    map[string][]string `yaml:"workflows"`
	Dependencies map[string]struct {
		Commit string `yaml:"commit"`
	} `yaml:"dependencies"`
}

// Locks reports whether action@ref used in workflowPath is pinned by the lock.
// A nil lock locks nothing.
func (l *ActionsLock) Locks(workflowPath, action, ref string) bool {
	if l == nil {
		return false
	}
	key := lockKey(action, ref)
	locked := false
	for k, dep := range l.Dependencies {
		if dep.Commit != "" && strings.EqualFold(k, key) {
			locked = true
			break
		}
	}
	if !locked {
		return false
	}
	for _, k := range l.Workflows[workflowPath] {
		if strings.EqualFold(k, key) {
			return true
		}
	}
	return false
}

// lockKey builds the lockfile key for action@ref the way `gh actions-lock`
// writes it: lowercase owner/repo (sub-path dropped), ref as-is.
func lockKey(action, ref string) string {
	parts := strings.SplitN(action, "/", 3)
	if len(parts) >= 2 {
		action = parts[0] + "/" + parts[1]
	}
	return strings.ToLower(action) + "@" + ref
}

// ParseActionsLock parses a `gh actions-lock` lockfile.
func ParseActionsLock(content []byte) (*ActionsLock, error) {
	var l ActionsLock
	if err := yaml.Unmarshal(content, &l); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", ActionsLockPath, err)
	}
	return &l, nil
}

// LoadActionsLock fetches and parses the repo's actions lockfile, returning
// nil (no error) when the repo has none.
func LoadActionsLock(ctx context.Context, client *github.Client, repo gh.Repository, ref string) (*ActionsLock, error) {
	content, err := gh.CachedFetchFileContent(ctx, client, repo.Owner, repo.Name, ActionsLockPath, ref)
	if err != nil || content == nil {
		return nil, err
	}
	return ParseActionsLock(content)
}

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
// (./...) and docker:// actions, references pinned by lock (nil for none)
// for workflowPath, and lines suppressed with a git-cascade:allow comment.
// Exported so the actions-pinned remediator can reuse the exact same
// detection logic as this checker.
func FindUnpinnedActions(content, workflowPath string, lock *ActionsLock) []UnpinnedAction {
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
		if shaRef.MatchString(actionRef) || lock.Locks(workflowPath, action, actionRef) {
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

	lock, err := LoadActionsLock(ctx, client, repo, ref)
	if err != nil {
		return nil, err
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

		for _, v := range FindUnpinnedActions(string(content), entry.GetPath(), lock) {
			violations = append(violations, fmt.Sprintf("%s: %s@%s", name, v.Action, v.Ref))
		}
	}

	if len(violations) > 0 {
		msg := fmt.Sprintf("%d action(s) not pinned to SHA or %s: %s", len(violations), ActionsLockPath, strings.Join(violations, "; "))
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
		Message:  "all actions pinned to SHA or " + ActionsLockPath,
	}, nil
}

func init() {
	compliance.Register(&actionsPinnedChecker{})
}
