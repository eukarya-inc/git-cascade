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

func makeRuleWithScope(id string, enabled bool, scope *config.RuleScope) config.Rule {
	r := makeRule(id, enabled)
	r.Scope = scope
	return r
}

func TestEngine_RuleScopeExcludeRepos(t *testing.T) {
	stub := &stubChecker{id: "scoped-rule-exclude"}
	Register(stub)
	defer delete(registry, "scoped-rule-exclude")

	cfg := &config.ComplianceConfig{
		Version: "1",
		Rules: []config.Rule{
			makeRuleWithScope("scoped-rule-exclude", true, &config.RuleScope{
				ExcludeRepos: []string{"testing", "sandbox-*"},
			}),
		},
	}
	repos := []gh.Repository{makeRepo("api"), makeRepo("testing"), makeRepo("sandbox-foo")}
	engine := NewEngine(nil, cfg, noopLogger())
	results, err := engine.Run(context.Background(), repos)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result (only api), got %d", len(results))
	}
	if results[0].Repo != "org/api" {
		t.Errorf("expected result for org/api, got %s", results[0].Repo)
	}
}

func TestEngine_RuleScopeIncludeRepos(t *testing.T) {
	stub := &stubChecker{id: "scoped-rule-include"}
	Register(stub)
	defer delete(registry, "scoped-rule-include")

	cfg := &config.ComplianceConfig{
		Version: "1",
		Rules: []config.Rule{
			makeRuleWithScope("scoped-rule-include", true, &config.RuleScope{
				IncludeRepos: []string{"super-*", "critical"},
			}),
		},
	}
	repos := []gh.Repository{makeRepo("api"), makeRepo("super-important"), makeRepo("critical")}
	engine := NewEngine(nil, cfg, noopLogger())
	results, err := engine.Run(context.Background(), repos)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results (super-important, critical), got %d", len(results))
	}
}

func TestEngine_RuleScopeIncludeOverridesExclude(t *testing.T) {
	stub := &stubChecker{id: "scoped-rule-both"}
	Register(stub)
	defer delete(registry, "scoped-rule-both")

	cfg := &config.ComplianceConfig{
		Version: "1",
		Rules: []config.Rule{
			makeRuleWithScope("scoped-rule-both", true, &config.RuleScope{
				IncludeRepos: []string{"api"},
				ExcludeRepos: []string{"api"}, // include wins
			}),
		},
	}
	repos := []gh.Repository{makeRepo("api"), makeRepo("web")}
	engine := NewEngine(nil, cfg, noopLogger())
	results, err := engine.Run(context.Background(), repos)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 1 || results[0].Repo != "org/api" {
		t.Errorf("expected 1 result for org/api (include wins), got %v", results)
	}
}

func TestEngine_RuleScopeNilRunsOnAll(t *testing.T) {
	stub := &stubChecker{id: "unscoped-rule"}
	Register(stub)
	defer delete(registry, "unscoped-rule")

	cfg := &config.ComplianceConfig{
		Version: "1",
		Rules:   []config.Rule{makeRule("unscoped-rule", true)},
	}
	repos := []gh.Repository{makeRepo("a"), makeRepo("b"), makeRepo("c")}
	engine := NewEngine(nil, cfg, noopLogger())
	results, err := engine.Run(context.Background(), repos)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results (no scope = all repos), got %d", len(results))
	}
}

func TestEngine_IncludeRulesGlob(t *testing.T) {
	stub1 := &stubChecker{id: "security-secrets"}
	stub2 := &stubChecker{id: "security-branch"}
	stub3 := &stubChecker{id: "quality-lint"}
	Register(stub1)
	Register(stub2)
	Register(stub3)
	defer delete(registry, "security-secrets")
	defer delete(registry, "security-branch")
	defer delete(registry, "quality-lint")

	cfg := &config.ComplianceConfig{
		Version: "1",
		Rules: []config.Rule{
			makeRule("security-secrets", true),
			makeRule("security-branch", true),
			makeRule("quality-lint", true),
		},
		IncludeRules: []string{"security-*"},
	}
	repos := []gh.Repository{makeRepo("a")}
	engine := NewEngine(nil, cfg, noopLogger())
	results, err := engine.Run(context.Background(), repos)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results (security-* rules only), got %d", len(results))
	}
	if stub3.calls.Load() != 0 {
		t.Error("quality-lint should not run when include_rules=security-*")
	}
}

func TestEngine_ExcludeRulesGlob(t *testing.T) {
	stub1 := &stubChecker{id: "sec-main"}
	stub2 := &stubChecker{id: "sec-extra"}
	stub3 := &stubChecker{id: "other-rule"}
	Register(stub1)
	Register(stub2)
	Register(stub3)
	defer delete(registry, "sec-main")
	defer delete(registry, "sec-extra")
	defer delete(registry, "other-rule")

	cfg := &config.ComplianceConfig{
		Version: "1",
		Rules: []config.Rule{
			makeRule("sec-main", true),
			makeRule("sec-extra", true),
			makeRule("other-rule", true),
		},
		ExcludeRules: []string{"sec-*"},
	}
	repos := []gh.Repository{makeRepo("a")}
	engine := NewEngine(nil, cfg, noopLogger())
	results, err := engine.Run(context.Background(), repos)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result (only other-rule), got %d", len(results))
	}
	if stub1.calls.Load() != 0 || stub2.calls.Load() != 0 {
		t.Error("sec-* rules should not run when exclude_rules=sec-*")
	}
}

func TestEngine_IncludeRulesTakesPrecedenceOverExclude(t *testing.T) {
	stub := &stubChecker{id: "overlap-rule"}
	Register(stub)
	defer delete(registry, "overlap-rule")

	cfg := &config.ComplianceConfig{
		Version: "1",
		Rules:   []config.Rule{makeRule("overlap-rule", true)},
		// include wins: rule matches include so it should run despite also matching exclude
		IncludeRules: []string{"overlap-*"},
		ExcludeRules: []string{"overlap-*"},
	}
	engine := NewEngine(nil, cfg, noopLogger())
	results, err := engine.Run(context.Background(), []gh.Repository{makeRepo("a")})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result (include_rules wins), got %d", len(results))
	}
}

func TestEngine_EmptyRepository_SkipsCheckers(t *testing.T) {
	stub := &stubChecker{id: "empty-repo-rule"}
	Register(stub)
	defer delete(registry, "empty-repo-rule")

	cfg := &config.ComplianceConfig{
		Version: "1",
		Rules:   []config.Rule{makeRule("empty-repo-rule", true)},
	}
	emptyRepo := gh.Repository{Owner: "org", Name: "empty", FullName: "org/empty", DefaultBranch: ""}
	engine := NewEngine(nil, cfg, noopLogger())
	results, err := engine.Run(context.Background(), []gh.Repository{emptyRepo})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stub.calls.Load() != 0 {
		t.Error("checker should not be called for an empty repository")
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 synthetic result, got %d", len(results))
	}
	r := results[0]
	if r.RuleID != "empty-repository" {
		t.Errorf("expected rule_id empty-repository, got %q", r.RuleID)
	}
	if r.Status != StatusFail {
		t.Errorf("expected StatusFail, got %s", r.Status)
	}
	if r.Severity != config.SeverityWarning {
		t.Errorf("expected SeverityWarning, got %s", r.Severity)
	}
	if r.Repo != "org/empty" {
		t.Errorf("expected repo org/empty, got %s", r.Repo)
	}
}

func TestEngine_MixedEmptyAndNonEmpty(t *testing.T) {
	stub := &stubChecker{id: "mixed-rule"}
	Register(stub)
	defer delete(registry, "mixed-rule")

	cfg := &config.ComplianceConfig{
		Version: "1",
		Rules:   []config.Rule{makeRule("mixed-rule", true)},
	}
	repos := []gh.Repository{
		makeRepo("normal"),
		{Owner: "org", Name: "empty", FullName: "org/empty", DefaultBranch: ""},
	}
	engine := NewEngine(nil, cfg, noopLogger())
	results, err := engine.Run(context.Background(), repos)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// normal repo gets 1 checker result; empty repo gets 1 synthetic result
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if stub.calls.Load() != 1 {
		t.Errorf("checker should be called once (for normal repo only), got %d", stub.calls.Load())
	}
	var hasEmpty, hasNormal bool
	for _, r := range results {
		if r.RuleID == "empty-repository" && r.Repo == "org/empty" {
			hasEmpty = true
		}
		if r.RuleID == "mixed-rule" && r.Repo == "org/normal" {
			hasNormal = true
		}
	}
	if !hasEmpty {
		t.Error("expected synthetic empty-repository result for org/empty")
	}
	if !hasNormal {
		t.Error("expected checker result for org/normal")
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
