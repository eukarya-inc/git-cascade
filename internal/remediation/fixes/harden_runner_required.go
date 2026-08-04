package fixes

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/compliance/checks"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/eukarya-inc/git-cascade/internal/remediation"
	"github.com/google/go-github/v84/github"
)

// hardenRunnerRef is the tag resolved to a commit SHA when inserting the
// harden-runner step. Pinned to a major version rather than an exact release
// so the fixer doesn't need updating every harden-runner release.
const hardenRunnerRef = "v2"

type hardenRunnerFixer struct{}

func (f *hardenRunnerFixer) ID() string { return "harden-runner-required" }

func (f *hardenRunnerFixer) Remediate(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule, result compliance.Result) (*remediation.Fix, error) {
	ref := repo.DefaultBranch

	dirContent, err := gh.CachedListDirectoryContents(ctx, client, repo.Owner, repo.Name, ".github/workflows", ref)
	if err != nil {
		return nil, err
	}

	var files []remediation.FileChange
	var fixedDescs []string
	var sha string // resolved once, reused across files in this repo
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

		missing := checks.FindJobsMissingHardenRunner(string(content))
		if len(missing) == 0 {
			continue
		}

		if sha == "" {
			sha, _, err = client.Repositories.GetCommitSHA1(ctx, "step-security", "harden-runner", hardenRunnerRef, "")
			if err != nil {
				continue // can't resolve the pin — leave this and later files untouched this run
			}
		}

		// Insert bottom-to-top so earlier StepsLine indices stay valid.
		sort.Slice(missing, func(i, j int) bool { return missing[i].StepsLine > missing[j].StepsLine })
		lines := strings.Split(string(content), "\n")
		var jobNames []string
		for _, m := range missing {
			lines = insertHardenRunnerStep(lines, m.StepsLine, sha)
			jobNames = append(jobNames, m.Job)
		}
		sort.Strings(jobNames) // restore readable order after the reverse insertion pass
		files = append(files, remediation.FileChange{Path: entry.GetPath(), Content: []byte(strings.Join(lines, "\n"))})
		fixedDescs = append(fixedDescs, fmt.Sprintf("%s: %s", name, strings.Join(jobNames, ", ")))
	}

	if len(files) == 0 {
		return nil, nil
	}

	return &remediation.Fix{
		Files:         files,
		CommitMessage: "fix(security): add step-security/harden-runner as first step",
		PRTitle:       "git-cascade: add step-security/harden-runner to workflow jobs",
		PRBody: fmt.Sprintf(
			"Automated fix for the `harden-runner-required` compliance rule.\n\nAdded `step-security/harden-runner@%s` as the first step of:\n- %s\n\n_Opened automatically by git-cascade auto-remediation._",
			hardenRunnerRef, strings.Join(fixedDescs, "\n- "),
		),
	}, nil
}

// insertHardenRunnerStep inserts a harden-runner step right after the job's
// `steps:` line (indent 6, matching the list-item indentation the rest of
// this codebase's YAML parsing assumes for workflow steps).
func insertHardenRunnerStep(lines []string, stepsLine int, sha string) []string {
	const indent = "      "
	step := []string{
		indent + "- name: Harden Runner",
		indent + "  uses: step-security/harden-runner@" + sha + " # " + hardenRunnerRef,
		indent + "  with:",
		indent + "    egress-policy: audit",
	}
	out := make([]string, 0, len(lines)+len(step))
	out = append(out, lines[:stepsLine+1]...)
	out = append(out, step...)
	out = append(out, lines[stepsLine+1:]...)
	return out
}

func init() {
	remediation.Register(&hardenRunnerFixer{})
}
