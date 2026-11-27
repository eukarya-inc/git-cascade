package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/go-github/v90/github"
)

// Repository is a minimal representation of a GitHub repository.
type Repository struct {
	Owner         string
	Name          string
	FullName      string
	DefaultBranch string
	Archived      bool
	Private       bool
	Fork          bool
}

// ListOrgRepos returns all repositories for a given GitHub organization.
func ListOrgRepos(ctx context.Context, client *github.Client, org string) ([]Repository, error) {
	var all []Repository
	opts := &github.RepositoryListByOrgOptions{
		Type:        "all",
		ListOptions: github.ListOptions{PerPage: 100},
	}

	for {
		repos, resp, err := client.Repositories.ListByOrg(ctx, org, opts)
		if err != nil {
			if waitForRateLimit(ctx, err) {
				continue
			}
			return nil, fmt.Errorf("listing repos for org %s: %w", org, err)
		}
		for _, r := range repos {
			all = append(all, Repository{
				Owner:         r.GetOwner().GetLogin(),
				Name:          r.GetName(),
				FullName:      r.GetFullName(),
				DefaultBranch: r.GetDefaultBranch(),
				Archived:      r.GetArchived(),
				Private:       r.GetPrivate(),
				Fork:          r.GetFork(),
			})
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return all, nil
}

// FetchFileContent retrieves the content of a file from a repository.
// For files larger than 1 MB the GitHub API returns encoding=none; in that
// case we fall back to DownloadContents which streams the raw blob.
// Rate limit errors are retried after waiting for the reset window.
func FetchFileContent(ctx context.Context, client *github.Client, owner, repo, path, ref string) ([]byte, error) {
	opts := &github.RepositoryContentGetOptions{Ref: ref}

	for attempt := 0; ; attempt++ {
		fileContent, _, resp, err := client.Repositories.GetContents(ctx, owner, repo, path, opts)
		if err != nil {
			if waitForRateLimit(ctx, err) {
				continue
			}
			if waitForTransient(ctx, err, attempt) {
				continue
			}
			if resp != nil && resp.StatusCode == 404 {
				return nil, nil // file does not exist
			}
			return nil, fmt.Errorf("fetching %s from %s/%s: %w", path, owner, repo, err)
		}
		if fileContent == nil {
			return nil, nil
		}

		content, err := fileContent.GetContent()
		if err != nil {
			// encoding=none means file is too large for the contents API; stream it instead.
			if !strings.Contains(err.Error(), "unsupported content encoding") {
				return nil, fmt.Errorf("decoding content of %s from %s/%s: %w", path, owner, repo, err)
			}
			rc, _, err := client.Repositories.DownloadContents(ctx, owner, repo, path, opts)
			if err != nil {
				return nil, fmt.Errorf("downloading %s from %s/%s: %w", path, owner, repo, err)
			}
			defer rc.Close()
			data, err := io.ReadAll(rc)
			if err != nil {
				return nil, fmt.Errorf("reading %s from %s/%s: %w", path, owner, repo, err)
			}
			return data, nil
		}

		return []byte(content), nil
	}
}

// ListDirectoryContents returns the directory entries at the given path.
// Returns nil (no error) if the path does not exist (404).
// Rate limit errors are retried after waiting for the reset window.
func ListDirectoryContents(ctx context.Context, client *github.Client, owner, repo, path, ref string) ([]*github.RepositoryContent, error) {
	opts := &github.RepositoryContentGetOptions{Ref: ref}
	for attempt := 0; ; attempt++ {
		_, dirContent, resp, err := client.Repositories.GetContents(ctx, owner, repo, path, opts)
		if err != nil {
			if waitForRateLimit(ctx, err) {
				continue
			}
			if waitForTransient(ctx, err, attempt) {
				continue
			}
			if resp != nil && resp.StatusCode == 404 {
				return nil, nil
			}
			return nil, fmt.Errorf("listing %s in %s/%s: %w", path, owner, repo, err)
		}
		return dirContent, nil
	}
}

// GetBranchRules returns the effective rules active on a branch, merged from
// all applicable rulesets (repository, organization, enterprise).
// Returns (nil, statusCode, nil) for non-retryable HTTP errors so callers can
// inspect the status code (404 = no rules, 403 = plan/permission limit).
// Rate limit errors are retried after waiting for the reset window.
func GetBranchRules(ctx context.Context, client *github.Client, owner, repo, branch string) (*github.BranchRules, int, error) {
	for {
		rules, resp, err := client.Repositories.ListRulesForBranch(ctx, owner, repo, branch, nil)
		if err != nil {
			if waitForRateLimit(ctx, err) {
				continue
			}
			if resp != nil {
				return nil, resp.StatusCode, nil
			}
			return nil, 0, fmt.Errorf("fetching branch rules for %s/%s:%s: %w", owner, repo, branch, err)
		}
		return rules, 0, nil
	}
}

// GetBranchProtection fetches branch protection rules.
// Returns (nil, statusCode, nil) when the API returns a non-retryable HTTP error
// so callers can inspect the status code (e.g. 404 = not enabled, 403 = plan limit).
// Rate limit errors are retried after waiting for the reset window.
func GetBranchProtection(ctx context.Context, client *github.Client, owner, repo, branch string) (*github.Protection, int, error) {
	for {
		protection, resp, err := client.Repositories.GetBranchProtection(ctx, owner, repo, branch)
		if err != nil {
			if waitForRateLimit(ctx, err) {
				continue
			}
			if resp != nil {
				return nil, resp.StatusCode, nil
			}
			return nil, 0, fmt.Errorf("fetching branch protection for %s/%s:%s: %w", owner, repo, branch, err)
		}
		return protection, 0, nil
	}
}

// ListCollaborators returns all collaborators for a repository with the given affiliation.
// Rate limit errors are retried after waiting for the reset window.
func ListCollaborators(ctx context.Context, client *github.Client, owner, repo, affiliation string) ([]*github.User, int, error) {
	opts := &github.ListCollaboratorsOptions{
		Affiliation: affiliation,
		ListOptions: github.ListOptions{PerPage: 100},
	}
	var all []*github.User
	for {
		collabs, resp, err := client.Repositories.ListCollaborators(ctx, owner, repo, opts)
		if err != nil {
			if waitForRateLimit(ctx, err) {
				continue
			}
			if resp != nil {
				return nil, resp.StatusCode, nil
			}
			return nil, 0, fmt.Errorf("listing collaborators for %s/%s: %w", owner, repo, err)
		}
		all = append(all, collabs...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return all, 0, nil
}

// GetBranchHEAD returns the SHA of the HEAD commit on the given branch.
// Returns an empty string (no error) when the branch or repository does not exist.
func GetBranchHEAD(ctx context.Context, client *github.Client, owner, repo, branch string) (string, error) {
	ref, resp, err := client.Git.GetRef(ctx, owner, repo, "refs/heads/"+branch)
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return "", nil
		}
		return "", fmt.Errorf("fetching HEAD for %s/%s@%s: %w", owner, repo, branch, err)
	}
	return ref.GetObject().GetSHA(), nil
}

// waitForRateLimit checks if err is a rate limit error and, if so, blocks until
// the reset time (plus one second) or the context is cancelled.
// Returns true if the caller should retry, false otherwise.
func waitForRateLimit(ctx context.Context, err error) bool {
	var rle *github.RateLimitError
	if !errors.As(err, &rle) {
		return false
	}
	resetIn := max(time.Until(rle.Rate.Reset.Time), 0)
	select {
	case <-ctx.Done():
		return false
	case <-time.After(resetIn + time.Second):
		return true
	}
}

// isTransientError reports whether err is a transient network failure that is
// safe to retry (unexpected EOF, connection reset, etc.).
func isTransientError(err error) bool {
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "unexpected EOF") ||
		strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "EOF")
}

// waitForTransient blocks for a short backoff when err is a transient network
// error. Returns true if the caller should retry, false otherwise.
func waitForTransient(ctx context.Context, err error, attempt int) bool {
	if !isTransientError(err) {
		return false
	}
	backoff := min(time.Duration(1<<attempt)*time.Second, 8*time.Second) // 1s, 2s, 4s, 8s
	select {
	case <-ctx.Done():
		return false
	case <-time.After(backoff):
		return true
	}
}

// RepoTree is the recursive file listing of a repository at a ref.
type RepoTree struct {
	// Paths holds the path of every blob (file) in the tree.
	Paths []string
	// Truncated is true when GitHub cut the listing short (over 100,000
	// entries or 7 MB), so Paths is incomplete.
	Truncated bool
}

// ListTree returns every file path on branch via the recursive Git Trees API.
// The branch is first resolved to its HEAD commit, so branch names with
// characters that aren't safe in a URL path (the trees endpoint takes the ref
// in the path) still work. Returns nil (no error) for a missing branch or an
// empty repository (404/409).
func ListTree(ctx context.Context, client *github.Client, owner, repo, branch string) (*RepoTree, error) {
	var sha string
	for attempt := 0; ; attempt++ {
		ref, resp, err := client.Git.GetRef(ctx, owner, repo, "heads/"+branch)
		if err != nil {
			if waitForRateLimit(ctx, err) {
				continue
			}
			if waitForTransient(ctx, err, attempt) {
				continue
			}
			if resp != nil && (resp.StatusCode == 404 || resp.StatusCode == 409) {
				return nil, nil
			}
			return nil, fmt.Errorf("resolving %s in %s/%s: %w", branch, owner, repo, err)
		}
		sha = ref.GetObject().GetSHA()
		break
	}
	for attempt := 0; ; attempt++ {
		tree, resp, err := client.Git.GetTree(ctx, owner, repo, sha, true)
		if err != nil {
			if waitForRateLimit(ctx, err) {
				continue
			}
			if waitForTransient(ctx, err, attempt) {
				continue
			}
			if resp != nil && (resp.StatusCode == 404 || resp.StatusCode == 409) {
				return nil, nil
			}
			return nil, fmt.Errorf("listing tree of %s/%s at %s: %w", owner, repo, branch, err)
		}
		t := &RepoTree{Truncated: tree.GetTruncated()}
		for _, e := range tree.Entries {
			if e.GetType() == "blob" {
				t.Paths = append(t.Paths, e.GetPath())
			}
		}
		return t, nil
	}
}
