package compliance

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/eukarya-inc/git-cascade/internal/config"
	"github.com/google/go-github/v84/github"
)

const (
	// DefaultConfigRepo is the default repository name where compliance configs are stored.
	DefaultConfigRepo = "compliance"
	// DefaultConfigPath is the default directory within the compliance repo to look for configs.
	// An empty string means the root of the repository.
	DefaultConfigPath = ""
)

// LoadConfigFromRepo loads compliance configuration from a GitHub repository.
// It looks for YAML files in the specified path within the repository.
// An empty path scans the repository root.
func LoadConfigFromRepo(ctx context.Context, client *github.Client, owner, repo, path, ref string) (*config.ComplianceConfig, error) {
	displayPath := path
	if displayPath == "" {
		displayPath = "(root)"
	}

	_, dirContent, resp, err := client.Repositories.GetContents(ctx, owner, repo, path, &github.RepositoryContentGetOptions{Ref: ref})
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return nil, fmt.Errorf("config path %s not found in %s/%s", displayPath, owner, repo)
		}
		return nil, fmt.Errorf("listing config files in %s/%s/%s: %w", owner, repo, displayPath, err)
	}

	merged := &config.ComplianceConfig{}
	for _, entry := range dirContent {
		name := entry.GetName()
		ext := filepath.Ext(name)
		if ext != ".yml" && ext != ".yaml" {
			continue
		}

		filePath := strings.TrimPrefix(entry.GetPath(), "/")
		fileContent, _, _, err := client.Repositories.GetContents(ctx, owner, repo, filePath, &github.RepositoryContentGetOptions{Ref: ref})
		if err != nil {
			return nil, fmt.Errorf("fetching %s: %w", filePath, err)
		}

		content, err := fileContent.GetContent()
		if err != nil {
			return nil, fmt.Errorf("decoding %s: %w", filePath, err)
		}

		cfg, err := config.Parse([]byte(content))
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", filePath, err)
		}

		if merged.Version == "" {
			merged.Version = cfg.Version
		}
		merged.Rules = append(merged.Rules, cfg.Rules...)
	}

	if merged.Version == "" {
		return nil, fmt.Errorf("no valid config files found in %s/%s/%s", owner, repo, displayPath)
	}

	return merged, nil
}
