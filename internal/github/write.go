package github

import (
	"context"
	"fmt"

	"github.com/google/go-github/v90/github"
)

// FileWrite is a single file to create or overwrite in a commit produced by CommitFiles.
type FileWrite struct {
	Path    string
	Content []byte
}

// EnsureBranch creates newBranch pointing at fromSHA if it doesn't already
// exist. If it exists, it is left as-is — a prior remediation run may have
// already committed to it, and repeated scans should update that same
// branch/PR rather than reset its history. Returns the branch's resulting
// head SHA, for the caller to pass into CommitFiles — GitHub's Data API has
// read-after-write lag on freshly created refs, so a caller that immediately
// re-fetches a just-created ref can see a spurious 404.
func EnsureBranch(ctx context.Context, client *github.Client, owner, repo, newBranch, fromSHA string) (string, error) {
	ref, resp, err := client.Git.GetRef(ctx, owner, repo, "refs/heads/"+newBranch)
	if err == nil {
		return ref.GetObject().GetSHA(), nil
	}
	if resp == nil || resp.StatusCode != 404 {
		return "", fmt.Errorf("checking branch %s: %w", newBranch, err)
	}
	if _, _, err := client.Git.CreateRef(ctx, owner, repo, github.CreateRef{
		Ref: "refs/heads/" + newBranch,
		SHA: fromSHA,
	}); err != nil {
		return "", fmt.Errorf("creating branch %s: %w", newBranch, err)
	}
	return fromSHA, nil
}

// CommitFiles creates blobs for each file, a single tree layered on
// headSHA's tree, one commit, and fast-forwards branch to it. headSHA is the
// branch's current head commit SHA, as returned by EnsureBranch. Returns the
// new commit SHA, or "" if the resulting tree is identical to the current
// tree (nothing to commit).
func CommitFiles(ctx context.Context, client *github.Client, owner, repo, branch, headSHA, message string, author *github.CommitAuthor, files []FileWrite) (string, error) {
	headCommit, _, err := client.Git.GetCommit(ctx, owner, repo, headSHA)
	if err != nil {
		return "", fmt.Errorf("fetching head commit: %w", err)
	}

	blobMode, blobType := "100644", "blob"
	entries := make([]*github.TreeEntry, len(files))
	for i, f := range files {
		content := string(f.Content)
		entries[i] = &github.TreeEntry{
			Path:    &files[i].Path,
			Mode:    &blobMode,
			Type:    &blobType,
			Content: &content,
		}
	}
	tree, _, err := client.Git.CreateTree(ctx, owner, repo, headCommit.GetTree().GetSHA(), entries)
	if err != nil {
		return "", fmt.Errorf("creating tree: %w", err)
	}
	if tree.GetSHA() == headCommit.GetTree().GetSHA() {
		return "", nil
	}

	commit := github.Commit{
		Message: &message,
		Tree:    tree,
		Parents: []*github.Commit{{SHA: &headSHA}},
	}
	if author != nil {
		commit.Author = author
		commit.Committer = author
	}
	newCommit, _, err := client.Git.CreateCommit(ctx, owner, repo, commit, nil)
	if err != nil {
		return "", fmt.Errorf("creating commit: %w", err)
	}

	force := true
	if _, _, err := client.Git.UpdateRef(ctx, owner, repo, "refs/heads/"+branch, github.UpdateRef{
		SHA:   newCommit.GetSHA(),
		Force: &force,
	}); err != nil {
		return "", fmt.Errorf("updating branch %s: %w", branch, err)
	}
	return newCommit.GetSHA(), nil
}

// CreateOrUpdatePullRequest opens a PR from head into base, or returns the
// URL of an existing open PR with the same head if one is already there —
// an upsert so re-running remediation doesn't create duplicate PRs.
func CreateOrUpdatePullRequest(ctx context.Context, client *github.Client, owner, repo, head, base, title, body string, labels []string, draft bool) (string, error) {
	existing, _, err := client.PullRequests.List(ctx, owner, repo, &github.PullRequestListOptions{
		State: "open",
		Head:  owner + ":" + head,
		Base:  base,
	})
	if err != nil {
		return "", fmt.Errorf("listing pull requests: %w", err)
	}
	if len(existing) > 0 {
		return existing[0].GetHTMLURL(), nil
	}

	pr, _, err := client.PullRequests.Create(ctx, owner, repo, github.CreatePullRequest{
		Title: &title,
		Head:  head,
		Base:  base,
		Body:  &body,
		Draft: &draft,
	})
	if err != nil {
		return "", fmt.Errorf("creating pull request: %w", err)
	}
	if len(labels) > 0 {
		if _, _, err := client.Issues.AddLabelsToIssue(ctx, owner, repo, pr.GetNumber(), labels); err != nil {
			return "", fmt.Errorf("labeling pull request: %w", err)
		}
	}
	return pr.GetHTMLURL(), nil
}
