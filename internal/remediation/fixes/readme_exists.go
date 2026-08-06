package fixes

import (
	"context"
	"fmt"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/eukarya-inc/git-cascade/internal/remediation"
	"github.com/google/go-github/v90/github"
)

type readmeExistsFixer struct{}

func (f *readmeExistsFixer) ID() string { return "readme-exists" }

// Remediate adds a minimal README.md stub. The engine only dispatches here on
// a failing result, meaning the checker already confirmed none of
// README.md/README/README.rst/readme.md exist, so there's no existing content
// to preserve or merge — a single new file is always the right fix.
func (f *readmeExistsFixer) Remediate(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule, result compliance.Result) (*remediation.Fix, error) {
	content := fmt.Sprintf("# %s\n\nTODO: describe this project.\n", repo.Name)

	return &remediation.Fix{
		Files: []remediation.FileChange{
			{Path: "README.md", Content: []byte(content)},
		},
		CommitMessage: "docs: add README",
		PRTitle:       "fix(readme-exists): add README.md",
		PRBody:        "Automated fix for the `readme-exists` compliance rule.\n\nAdded a minimal README.md stub — please expand it with a real project description.\n\n_Opened automatically by git-cascade auto-remediation._",
	}, nil
}

func init() {
	remediation.Register(&readmeExistsFixer{})
}
