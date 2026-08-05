package checks

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/google/go-github/v90/github"
)

// manifestToLockfile maps manifest files to their expected lockfiles.
var manifestToLockfile = map[string][]string{
	"package.json":   {"package-lock.json", "yarn.lock", "pnpm-lock.yaml", "bun.lockb"},
	"go.mod":        {"go.sum"},
	"Cargo.toml":    {"Cargo.lock"},
	"pyproject.toml": {"uv.lock", "poetry.lock", "requirements.txt"},
}

type lockfileRequiredChecker struct{}

func (c *lockfileRequiredChecker) ID() string { return "lockfile-required" }

func (c *lockfileRequiredChecker) Check(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule) (*compliance.Result, error) {
	ref := repo.DefaultBranch

	type ecosystemResult struct {
		manifest string
		missing  bool
		lockfiles []string
	}

	resultCh := make(chan ecosystemResult, len(manifestToLockfile))
	errCh := make(chan error, len(manifestToLockfile))

	var wg sync.WaitGroup
	for manifest, lockfiles := range manifestToLockfile {
		wg.Add(1)
		go func() {
			defer wg.Done()

			manifestContent, err := gh.CachedFetchFileContent(ctx, client, repo.Owner, repo.Name, manifest, ref)
			if err != nil {
				errCh <- err
				return
			}
			if manifestContent == nil {
				return // ecosystem not used in this repo
			}

			foundLock, err := fetchFirstExisting(ctx, client, repo.Owner, repo.Name, ref, lockfiles)
			if err != nil {
				errCh <- err
				return
			}
			resultCh <- ecosystemResult{
				manifest:  manifest,
				missing:   foundLock == "",
				lockfiles: lockfiles,
			}
		}()
	}

	wg.Wait()
	close(resultCh)
	close(errCh)

	if err := <-errCh; err != nil {
		return nil, err
	}

	var missing []string
	for r := range resultCh {
		if r.missing {
			missing = append(missing, fmt.Sprintf("%s (expected one of: %s)", r.manifest, strings.Join(r.lockfiles, ", ")))
		}
	}

	if len(missing) > 0 {
		sort.Strings(missing) // deterministic output
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusFail,
			Severity: rule.Severity,
			Message:  fmt.Sprintf("missing lockfile for: %s", strings.Join(missing, "; ")),
		}, nil
	}

	return &compliance.Result{
		RuleID:   rule.ID,
		RuleName: rule.Name,
		Repo:     repo.FullName,
		Status:   compliance.StatusPass,
		Severity: rule.Severity,
		Message:  "all manifests have corresponding lockfiles",
	}, nil
}

func init() {
	compliance.Register(&lockfileRequiredChecker{})
}
