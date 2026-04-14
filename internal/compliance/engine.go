package compliance

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/google/go-github/v84/github"
)

// defaultConcurrency is the number of concurrent (rule, repo) checks.
// GitHub's secondary rate limit is ~900 requests/minute (15 req/sec) per
// installation. 5 workers stays safely under that ceiling even when every
// request completes instantly, while still providing meaningful parallelism.
const defaultConcurrency = 5

// Engine orchestrates compliance checks across repositories.
type Engine struct {
	client      *github.Client
	config      *config.ComplianceConfig
	logger      *slog.Logger
	concurrency int
}

// NewEngine creates a new compliance engine.
func NewEngine(client *github.Client, cfg *config.ComplianceConfig, logger *slog.Logger) *Engine {
	return &Engine{
		client:      client,
		config:      cfg,
		logger:      logger,
		concurrency: defaultConcurrency,
	}
}

// WithConcurrency sets the number of concurrent checks. Values <= 0 are ignored.
func (e *Engine) WithConcurrency(n int) *Engine {
	if n > 0 {
		e.concurrency = n
	}
	return e
}

type checkJob struct {
	rule    config.Rule
	repo    gh.Repository
	checker Checker
}

type checkOutcome struct {
	result *Result
	err    error
}

// Run executes all enabled compliance checks concurrently across repositories.
// Each (rule, repo) pair is an independent job dispatched to a worker pool.
func (e *Engine) Run(ctx context.Context, repos []gh.Repository) ([]Result, error) {
	// Collect active (checker, rule) pairs first
	type activeRule struct {
		rule    config.Rule
		checker Checker
	}
	var activeRules []activeRule
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
		activeRules = append(activeRules, activeRule{rule: rule, checker: checker})
	}

	// Interleave jobs repo-first so all rules for a given repo are adjacent.
	// This means workers pick up different rules for the same repo in parallel
	// rather than exhausting one rule across all repos before starting the next.
	var jobs []checkJob
	for _, repo := range repos {
		for _, ar := range activeRules {
			jobs = append(jobs, checkJob{rule: ar.rule, repo: repo, checker: ar.checker})
		}
	}

	if len(jobs) == 0 {
		return nil, nil
	}

	jobCh := make(chan checkJob, len(jobs))
	for _, j := range jobs {
		jobCh <- j
	}
	close(jobCh)

	outCh := make(chan checkOutcome, len(jobs))

	workers := min(e.concurrency, len(jobs))

	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for job := range jobCh {
				e.logger.Info("checking", "rule", job.rule.ID, "repo", job.repo.FullName)
				result, err := job.checker.Check(ctx, e.client, job.repo, job.rule)
				outCh <- checkOutcome{result: result, err: err}
			}
		}()
	}

	// Close outCh once all workers finish
	go func() {
		wg.Wait()
		close(outCh)
	}()

	// Build a lookup so we can stamp visibility onto results without
	// requiring every checker to know about the repo's Private field.
	repoPrivate := make(map[string]bool, len(repos))
	for _, r := range repos {
		repoPrivate[r.FullName] = r.Private
	}

	results := make([]Result, 0, len(jobs))
	for outcome := range outCh {
		if outcome.err != nil {
			return nil, fmt.Errorf("compliance check failed: %w", outcome.err)
		}
		outcome.result.Private = repoPrivate[outcome.result.Repo]
		results = append(results, *outcome.result)
	}

	return results, nil
}
