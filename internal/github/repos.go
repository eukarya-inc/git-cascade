package github

import (
	"context"
	"fmt"

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
func FetchFileContent(ctx context.Context, client *github.Client, owner, repo, path, ref string) ([]byte, error) {
	opts := &github.RepositoryContentGetOptions{Ref: ref}
	fileContent, _, resp, err := client.Repositories.GetContents(ctx, owner, repo, path, opts)
	if err != nil {
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
		return nil, fmt.Errorf("decoding content of %s from %s/%s: %w", path, owner, repo, err)
	}
	return []byte(content), nil
}
