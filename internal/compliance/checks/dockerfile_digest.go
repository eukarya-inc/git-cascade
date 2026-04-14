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

// fromLine matches FROM instructions in Dockerfiles.
// Captures: image reference (everything after FROM, optional --platform, up to AS or EOL).
var fromLine = regexp.MustCompile(`(?im)^FROM\s+(?:--platform=\S+\s+)?(\S+)`)

// sha256Digest matches a @sha256: pinned reference.
var sha256Digest = regexp.MustCompile(`@sha256:[0-9a-f]{64}`)

// dockerfilePaths are the common locations to check for Dockerfiles.
var dockerfilePaths = []string{
	"Dockerfile",
	"docker/Dockerfile",
}

type dockerfileDigestChecker struct{}

func (c *dockerfileDigestChecker) ID() string { return "dockerfile-digest" }

func (c *dockerfileDigestChecker) Check(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule) (*compliance.Result, error) {
	ref := repo.DefaultBranch

	// Also scan any Dockerfile.* variants in the root
	searchPaths := make([]string, len(dockerfilePaths))
	copy(searchPaths, dockerfilePaths)

	// Check root directory for Dockerfile* files
	_, dirContent, resp, err := client.Repositories.GetContents(ctx, repo.Owner, repo.Name, "", &github.RepositoryContentGetOptions{Ref: ref})
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return &compliance.Result{
				RuleID:   rule.ID,
				RuleName: rule.Name,
				Repo:     repo.FullName,
				Status:   compliance.StatusSkip,
				Severity: rule.Severity,
				Message:  "repository is empty",
			}, nil
		}
		return nil, fmt.Errorf("listing root directory for %s: %w", repo.FullName, err)
	}
	for _, entry := range dirContent {
		name := entry.GetName()
		if strings.HasPrefix(name, "Dockerfile") && name != "Dockerfile" {
			searchPaths = append(searchPaths, name)
		}
	}

	var violations []string
	found := false

	for _, path := range searchPaths {
		content, err := gh.FetchFileContent(ctx, client, repo.Owner, repo.Name, path, ref)
		if err != nil {
			return nil, err
		}
		if content == nil {
			continue
		}
		found = true

		matches := fromLine.FindAllStringSubmatch(string(content), -1)
		for _, m := range matches {
			image := m[1]
			// Skip build stage aliases (FROM build AS ...) and scratch
			if strings.EqualFold(image, "scratch") {
				continue
			}
			// Skip ARG-based references like ${BASE_IMAGE} — can't validate statically
			if strings.Contains(image, "$") {
				continue
			}
			if !sha256Digest.MatchString(image) {
				violations = append(violations, fmt.Sprintf("%s: FROM %s", path, image))
			}
		}
	}

	if !found {
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusSkip,
			Severity: rule.Severity,
			Message:  "no Dockerfiles found",
		}, nil
	}

	if len(violations) > 0 {
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusFail,
			Severity: rule.Severity,
			Message:  fmt.Sprintf("%d FROM image(s) not pinned to SHA256 digest: %s", len(violations), strings.Join(violations, "; ")),
		}, nil
	}

	return &compliance.Result{
		RuleID:   rule.ID,
		RuleName: rule.Name,
		Repo:     repo.FullName,
		Status:   compliance.StatusPass,
		Severity: rule.Severity,
		Message:  "all Dockerfile FROM images pinned to SHA256 digest",
	}, nil
}

func init() {
	compliance.Register(&dockerfileDigestChecker{})
}
