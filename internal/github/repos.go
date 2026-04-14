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
			var rle *github.RateLimitError
			if errors.As(err, &rle) {
				resetIn := max(time.Until(rle.Rate.Reset.Time), 0)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(resetIn + time.Second):
					continue
				}
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
// Rate limit errors are retried once after waiting for the reset window.
func FetchFileContent(ctx context.Context, client *github.Client, owner, repo, path, ref string) ([]byte, error) {
	opts := &github.RepositoryContentGetOptions{Ref: ref}

	for range 2 {
		fileContent, _, resp, err := client.Repositories.GetContents(ctx, owner, repo, path, opts)
		if err != nil {
			var rle *github.RateLimitError
			if errors.As(err, &rle) {
				resetIn := max(time.Until(rle.Rate.Reset.Time), 0)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(resetIn + time.Second):
					continue
				}
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

	return nil, fmt.Errorf("fetching %s from %s/%s: rate limit exceeded after retry", path, owner, repo)
}
