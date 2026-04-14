package compliance

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/google/go-github/v84/github"
)

// stubChecker is a Checker that records calls and returns a configurable result.
type stubChecker struct {
	id    string
	calls atomic.Int64
	fail  bool
}

func (s *stubChecker) ID() string { return s.id }
func (s *stubChecker) Check(_ context.Context, _ *github.Client, repo gh.Repository, rule config.Rule) (*Result, error) {
	s.calls.Add(1)
	status := StatusPass
	if s.fail {
		status = StatusFail
	}
	return &Result{
		RuleID:   rule.ID,
		RuleName: rule.Name,
		Repo:     repo.FullName,
		Status:   status,
		Severity: rule.Severity,
		Message:  "stub",
	}, nil
}

func makeRule(id string, enabled bool) config.Rule {
	return config.Rule{ID: id, Name: id, Severity: config.SeverityWarning, Enabled: enabled}
}

func makeRepo(name string) gh.Repository {
	return gh.Repository{Owner: "org", Name: name, FullName: "org/" + name, DefaultBranch: "main"}
}

func TestEngine_RunAllJobs(t *testing.T) {
	stub := &stubChecker{id: "stub-rule"}
	Register(stub)
	defer delete(registry, "stub-rule")

	cfg := &config.ComplianceConfig{
		Version: "1",
		Rules:   []config.Rule{makeRule("stub-rule", true)},
	}
	repos := []gh.Repository{makeRepo("a"), makeRepo("b"), makeRepo("c")}

	engine := NewEngine(nil, cfg, noopLogger())
	results, err := engine.Run(context.Background(), repos)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results (1 rule × 3 repos), got %d", len(results))
	}
	if stub.calls.Load() != 3 {
		t.Errorf("expected 3 checker calls, got %d", stub.calls.Load())
	}
}

func TestEngine_SkipsDisabledRules(t *testing.T) {
	stub := &stubChecker{id: "disabled-rule"}
	Register(stub)
	defer delete(registry, "disabled-rule")

	cfg := &config.ComplianceConfig{
		Version: "1",
		Rules:   []config.Rule{makeRule("disabled-rule", false)},
	}
	engine := NewEngine(nil, cfg, noopLogger())
	results, err := engine.Run(context.Background(), []gh.Repository{makeRepo("a")})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for disabled rule, got %d", len(results))
	}
	if stub.calls.Load() != 0 {
		t.Error("disabled rule checker should not be called")
	}
}

func TestEngine_SkipsUnregisteredRules(t *testing.T) {
	cfg := &config.ComplianceConfig{
		Version: "1",
		Rules:   []config.Rule{makeRule("no-such-checker", true)},
	}
	engine := NewEngine(nil, cfg, noopLogger())
	results, err := engine.Run(context.Background(), []gh.Repository{makeRepo("a")})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for unregistered rule, got %d", len(results))
	}
}

func TestEngine_MultipleRulesMultipleRepos(t *testing.T) {
	stub1 := &stubChecker{id: "rule-alpha"}
	stub2 := &stubChecker{id: "rule-beta"}
	Register(stub1)
	Register(stub2)
	defer delete(registry, "rule-alpha")
	defer delete(registry, "rule-beta")

	cfg := &config.ComplianceConfig{
		Version: "1",
		Rules: []config.Rule{
			makeRule("rule-alpha", true),
			makeRule("rule-beta", true),
		},
	}
	repos := []gh.Repository{makeRepo("x"), makeRepo("y")}

	engine := NewEngine(nil, cfg, noopLogger())
	results, err := engine.Run(context.Background(), repos)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// 2 rules × 2 repos = 4
	if len(results) != 4 {
		t.Errorf("expected 4 results, got %d", len(results))
	}
	if stub1.calls.Load() != 2 {
		t.Errorf("expected stub1 called 2 times, got %d", stub1.calls.Load())
	}
	if stub2.calls.Load() != 2 {
		t.Errorf("expected stub2 called 2 times, got %d", stub2.calls.Load())
	}
}

func TestEngine_EmptyRepos(t *testing.T) {
	stub := &stubChecker{id: "empty-repos-rule"}
	Register(stub)
	defer delete(registry, "empty-repos-rule")

	cfg := &config.ComplianceConfig{
		Version: "1",
		Rules:   []config.Rule{makeRule("empty-repos-rule", true)},
	}
	engine := NewEngine(nil, cfg, noopLogger())
	results, err := engine.Run(context.Background(), nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty repo list, got %d", len(results))
	}
}

func TestEngine_WithConcurrency(t *testing.T) {
	e := NewEngine(nil, &config.ComplianceConfig{Version: "1"}, noopLogger())
	if e.concurrency != defaultConcurrency {
		t.Errorf("expected default concurrency %d, got %d", defaultConcurrency, e.concurrency)
	}
	e.WithConcurrency(3)
	if e.concurrency != 3 {
		t.Errorf("expected concurrency 3, got %d", e.concurrency)
	}
	// Zero and negative are ignored
	e.WithConcurrency(0)
	if e.concurrency != 3 {
		t.Error("WithConcurrency(0) should be ignored")
	}
	e.WithConcurrency(-1)
	if e.concurrency != 3 {
		t.Error("WithConcurrency(-1) should be ignored")
	}
}
