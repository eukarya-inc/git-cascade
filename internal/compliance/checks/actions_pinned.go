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

// shaRef matches a full 40-char hex SHA.
var shaRef = regexp.MustCompile(`^[0-9a-f]{40}$`)

// usesPattern matches `uses: owner/repo@ref` lines in workflow YAML.
// It captures the ref portion after @.
var usesPattern = regexp.MustCompile(`uses:\s*['"]?([^@'"]+)@([^'"#\s]+)`)

type actionsPinnedChecker struct{}

func (c *actionsPinnedChecker) ID() string { return "actions-pinned" }

func (c *actionsPinnedChecker) Check(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule) (*compliance.Result, error) {
	ref := repo.DefaultBranch

	// List workflow files
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

		matches := usesPattern.FindAllStringSubmatch(string(content), -1)
		for _, m := range matches {
			action := m[1]
			actionRef := m[2]

			// Skip local actions (e.g. ./.github/actions/foo)
			if strings.HasPrefix(action, ".") {
				continue
			}
			// Skip docker:// references
			if strings.HasPrefix(action, "docker://") {
				continue
			}

			if !shaRef.MatchString(actionRef) {
				violations = append(violations, fmt.Sprintf("%s: %s@%s", name, action, actionRef))
			}
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
