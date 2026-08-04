package remediation

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/google/go-github/v84/github"
)

const defaultBranchPrefix = "git-cascade/fix"

// Outcome describes what happened for one (rule, repo) remediation attempt.
type Outcome struct {
	RuleID string
	Repo   string
	PRURL  string // empty when there was nothing to fix
	Err    error
}

// Run finds failing results whose effective auto_remediation is enabled and
// that have a registered Remediator, and opens or updates a fix PR for each.
// cfg.Enabled is checked as the overall gate — nothing runs when it's false,
// regardless of any rule's auto_remediation setting. rules must be keyed by
// rule ID and repos by repository full name.
func Run(ctx context.Context, client *github.Client, cfg config.RemediationConfig, results []compliance.Result, rules map[string]config.Rule, repos map[string]gh.Repository, logger *slog.Logger) []Outcome {
	if !cfg.Enabled {
		return nil
	}

	var outcomes []Outcome
	for _, result := range results {
		if result.Status != compliance.StatusFail {
			continue
		}
		rule, ok := rules[result.RuleID]
		if !ok || !config.EffectiveAutoRemediation(rule) {
			continue
		}
		remediator := Get(result.RuleID)
		if remediator == nil {
			continue
		}
		repo, ok := repos[result.Repo]
		if !ok {
			continue
		}

		logger.Info("remediating", "rule", result.RuleID, "repo", result.Repo)
		prURL, err := remediateOne(ctx, client, cfg, remediator, repo, rule, result)
		if err != nil {
			logger.Warn("remediation failed", "rule", result.RuleID, "repo", result.Repo, "error", err)
		}
		outcomes = append(outcomes, Outcome{RuleID: result.RuleID, Repo: result.Repo, PRURL: prURL, Err: err})
	}
	return outcomes
}

func remediateOne(ctx context.Context, client *github.Client, cfg config.RemediationConfig, remediator Remediator, repo gh.Repository, rule config.Rule, result compliance.Result) (string, error) {
	fix, err := remediator.Remediate(ctx, client, repo, rule, result)
	if err != nil {
		return "", fmt.Errorf("computing fix: %w", err)
	}
	if fix == nil || len(fix.Files) == 0 {
		return "", nil
	}

	branchPrefix := cfg.BranchPrefix
	if branchPrefix == "" {
		branchPrefix = defaultBranchPrefix
	}
	branch := fmt.Sprintf("%s/%s", branchPrefix, rule.ID)

	baseSHA, err := gh.GetBranchHEAD(ctx, client, repo.Owner, repo.Name, repo.DefaultBranch)
	if err != nil {
		return "", fmt.Errorf("resolving base branch HEAD: %w", err)
	}
	if baseSHA == "" {
		return "", fmt.Errorf("default branch %s not found", repo.DefaultBranch)
	}
	if err := gh.EnsureBranch(ctx, client, repo.Owner, repo.Name, branch, baseSHA); err != nil {
		return "", fmt.Errorf("ensuring branch: %w", err)
	}

	var author *github.CommitAuthor
	if cfg.CommitAuthor.Name != "" || cfg.CommitAuthor.Email != "" {
		author = &github.CommitAuthor{}
		if cfg.CommitAuthor.Name != "" {
			author.Name = &cfg.CommitAuthor.Name
		}
		if cfg.CommitAuthor.Email != "" {
			author.Email = &cfg.CommitAuthor.Email
		}
	}

	files := make([]gh.FileWrite, len(fix.Files))
	for i, f := range fix.Files {
		files[i] = gh.FileWrite{Path: f.Path, Content: f.Content}
	}
	newSHA, err := gh.CommitFiles(ctx, client, repo.Owner, repo.Name, branch, fix.CommitMessage, author, files)
	if err != nil {
		return "", fmt.Errorf("committing fix: %w", err)
	}
	if newSHA == "" {
		return "", nil
	}

	labels := cfg.PRLabels
	if labels == nil {
		labels = []string{}
	}
	return gh.CreateOrUpdatePullRequest(ctx, client, repo.Owner, repo.Name, branch, repo.DefaultBranch, fix.PRTitle, fix.PRBody, labels, cfg.DraftPR)
}
