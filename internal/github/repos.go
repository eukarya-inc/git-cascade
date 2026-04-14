package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/go-github/v84/github"
)

// Repository is a minimal representation of a GitHub repository.
type Repository struct {
	Owner         string
	Name          string
	FullName      string
	DefaultBranch string
	Archived      bool
	Private       bool
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

	for {
		fileContent, _, resp, err := client.Repositories.GetContents(ctx, owner, repo, path, opts)
		if err != nil {
			if waitForRateLimit(ctx, err) {
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
	for {
		_, dirContent, resp, err := client.Repositories.GetContents(ctx, owner, repo, path, opts)
		if err != nil {
			if waitForRateLimit(ctx, err) {
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
