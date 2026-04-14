package compliance

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/google/go-github/v84/github"
)

// Engine orchestrates compliance checks across repositories.
type Engine struct {
	client *github.Client
	config *config.ComplianceConfig
	logger *slog.Logger
}

// NewEngine creates a new compliance engine.
func NewEngine(client *github.Client, cfg *config.ComplianceConfig, logger *slog.Logger) *Engine {
	return &Engine{
		client: client,
		config: cfg,
		logger: logger,
	}
}

// Run executes all enabled compliance checks against the given repositories.
func (e *Engine) Run(ctx context.Context, repos []gh.Repository) ([]Result, error) {
	var results []Result

	for _, rule := range e.config.Rules {
		if !rule.Enabled {
			e.logger.Debug("skipping disabled rule", "rule", rule.ID)
			continue
		}

		checker := GetChecker(rule.ID)
		if checker == nil {
			e.logger.Warn("no checker registered for rule", "rule", rule.ID)
			continue
		}

		for _, repo := range repos {
			e.logger.Info("checking", "rule", rule.ID, "repo", repo.FullName)

			result, err := checker.Check(ctx, e.client, repo, rule)
			if err != nil {
				return nil, fmt.Errorf("checking rule %s on %s: %w", rule.ID, repo.FullName, err)
			}
			results = append(results, *result)
		}
	}

	return results, nil
}
