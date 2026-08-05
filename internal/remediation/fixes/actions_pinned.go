package fixes

import (
	"context"
	"fmt"
	"strings"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/compliance/checks"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/eukarya-inc/git-cascade/internal/remediation"
	"github.com/google/go-github/v84/github"
)

type actionsPinnedFixer struct{}

func (f *actionsPinnedFixer) ID() string { return "actions-pinned" }

func (f *actionsPinnedFixer) Remediate(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule, result compliance.Result) (*remediation.Fix, error) {
	ref := repo.DefaultBranch

	dirContent, err := gh.CachedListDirectoryContents(ctx, client, repo.Owner, repo.Name, ".github/workflows", ref)
	if err != nil {
		return nil, err
	}

	var files []remediation.FileChange
	var fixedRefs []string
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

		violations := checks.FindUnpinnedActions(string(content))
		if len(violations) == 0 {
			continue
		}

		newContent, fixed, err := rewriteUnpinnedActions(ctx, client, string(content), violations)
		if err != nil {
			return nil, fmt.Errorf("resolving SHAs for %s: %w", entry.GetPath(), err)
		}
		if len(fixed) == 0 {
			continue
		}
		files = append(files, remediation.FileChange{Path: entry.GetPath(), Content: []byte(newContent)})
		fixedRefs = append(fixedRefs, fixed...)
	}

	if len(files) == 0 {
		return nil, nil
	}

	return &remediation.Fix{
		Files:         files,
		CommitMessage: "fix(security): pin GitHub Actions to commit SHA",
		PRTitle:       "git-cascade: pin GitHub Actions to commit SHA",
		PRBody: fmt.Sprintf(
			"Automated fix for the `actions-pinned` compliance rule.\n\nResolved and pinned:\n- %s\n\n_Opened automatically by git-cascade auto-remediation._",
			strings.Join(fixedRefs, "\n- "),
		),
	}, nil
}

// rewriteUnpinnedActions resolves each violation's tag/branch ref to a full
// commit SHA via the GitHub API and rewrites its line in place, preserving
// the original ref as a trailing comment (e.g. `@<sha> # v4`). A violation
// whose action string isn't a resolvable owner/repo, or whose ref can't be
// resolved (deleted tag, renamed repo, etc.), is left untouched rather than
// failing the whole file.
func rewriteUnpinnedActions(ctx context.Context, client *github.Client, content string, violations []checks.UnpinnedAction) (string, []string, error) {
	lines := strings.Split(content, "\n")
	var fixed []string
	for _, v := range violations {
		parts := strings.SplitN(v.Action, "/", 3)
		if len(parts) < 2 {
			continue
		}
		owner, repo := parts[0], parts[1]

		sha, _, err := client.Repositories.GetCommitSHA1(ctx, owner, repo, v.Ref, "")
		if err != nil {
			continue
		}

		old := v.Action + "@" + v.Ref
		newRef := v.Action + "@" + sha + " # " + v.Ref
		if !strings.Contains(lines[v.Line], old) {
			continue
		}
		lines[v.Line] = strings.Replace(lines[v.Line], old, newRef, 1)
		fixed = append(fixed, fmt.Sprintf("`%s@%s` → `%s@%s`", v.Action, v.Ref, v.Action, sha))
	}
	if len(fixed) == 0 {
		return content, nil, nil
	}
	return strings.Join(lines, "\n"), fixed, nil
}

func init() {
	remediation.Register(&actionsPinnedFixer{})
}
