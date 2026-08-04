package remediation

import (
	"context"
	"log/slog"
	"testing"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/google/go-github/v84/github"
)

type fakeRemediator struct {
	id      string
	calls   int
	fix     *Fix
	fixFunc func(result compliance.Result) *Fix
}

func (f *fakeRemediator) ID() string { return f.id }

func (f *fakeRemediator) Remediate(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule, result compliance.Result) (*Fix, error) {
	f.calls++
	if f.fixFunc != nil {
		return f.fixFunc(result), nil
	}
	return f.fix, nil
}

func boolPtr(b bool) *bool { return &b }

func TestRun_SkipsEverythingWhenRemediationDisabled(t *testing.T) {
	r := &fakeRemediator{id: "test-rule"}
	Register(r)
	defer delete(registry, "test-rule")

	rules := map[string]config.Rule{"test-rule": {ID: "test-rule", AutoRemediation: boolPtr(true)}}
	results := []compliance.Result{{RuleID: "test-rule", Repo: "o/r", Status: compliance.StatusFail}}
	repos := map[string]gh.Repository{"o/r": {Owner: "o", Name: "r", FullName: "o/r"}}

	outcomes := Run(context.Background(), nil, config.RemediationConfig{Enabled: false}, results, rules, repos, slog.Default())
	if outcomes != nil {
		t.Errorf("expected no outcomes when remediation.enabled=false, got %v", outcomes)
	}
	if r.calls != 0 {
		t.Errorf("remediator should not be invoked when remediation.enabled=false, got %d calls", r.calls)
	}
}

func TestRun_SkipsRuleWithAutoRemediationDisabled(t *testing.T) {
	r := &fakeRemediator{id: "test-rule"}
	Register(r)
	defer delete(registry, "test-rule")

	rules := map[string]config.Rule{"test-rule": {ID: "test-rule", AutoRemediation: boolPtr(false)}}
	results := []compliance.Result{{RuleID: "test-rule", Repo: "o/r", Status: compliance.StatusFail}}
	repos := map[string]gh.Repository{"o/r": {Owner: "o", Name: "r", FullName: "o/r"}}

	Run(context.Background(), nil, config.RemediationConfig{Enabled: true}, results, rules, repos, slog.Default())
	if r.calls != 0 {
		t.Errorf("rule-level auto_remediation=false should be honored, got %d calls", r.calls)
	}
}

func TestRun_SkipsRuleWithNoAutoRemediationField(t *testing.T) {
	r := &fakeRemediator{id: "test-rule"}
	Register(r)
	defer delete(registry, "test-rule")

	// No auto_remediation field at all — must default to false even though
	// remediation.enabled is true. There is no block-level default to fall
	// back to; each rule must opt in explicitly.
	rules := map[string]config.Rule{"test-rule": {ID: "test-rule"}}
	results := []compliance.Result{{RuleID: "test-rule", Repo: "o/r", Status: compliance.StatusFail}}
	repos := map[string]gh.Repository{"o/r": {Owner: "o", Name: "r", FullName: "o/r"}}

	Run(context.Background(), nil, config.RemediationConfig{Enabled: true}, results, rules, repos, slog.Default())
	if r.calls != 0 {
		t.Errorf("a rule with no auto_remediation field should not be remediated, got %d calls", r.calls)
	}
}

func TestRun_SkipsPassingResultsAndUnregisteredRules(t *testing.T) {
	r := &fakeRemediator{id: "test-rule"}
	Register(r)
	defer delete(registry, "test-rule")

	rules := map[string]config.Rule{
		"test-rule":     {ID: "test-rule", AutoRemediation: boolPtr(true)},
		"no-remediator": {ID: "no-remediator", AutoRemediation: boolPtr(true)},
	}
	results := []compliance.Result{
		{RuleID: "test-rule", Repo: "o/r", Status: compliance.StatusPass},
		{RuleID: "no-remediator", Repo: "o/r", Status: compliance.StatusFail},
	}
	repos := map[string]gh.Repository{"o/r": {Owner: "o", Name: "r", FullName: "o/r"}}

	outcomes := Run(context.Background(), nil, config.RemediationConfig{Enabled: true}, results, rules, repos, slog.Default())
	if len(outcomes) != 0 {
		t.Errorf("expected no outcomes (pass result + no remediator registered), got %v", outcomes)
	}
	if r.calls != 0 {
		t.Errorf("remediator should not be called for a passing result, got %d calls", r.calls)
	}
}

func TestRun_CallsRemediatorForFailingEnabledRule(t *testing.T) {
	r := &fakeRemediator{id: "test-rule", fix: nil} // nil fix -> nothing to commit, but Remediate must still be invoked
	Register(r)
	defer delete(registry, "test-rule")

	rules := map[string]config.Rule{"test-rule": {ID: "test-rule", AutoRemediation: boolPtr(true)}}
	results := []compliance.Result{{RuleID: "test-rule", Repo: "o/r", Status: compliance.StatusFail}}
	repos := map[string]gh.Repository{"o/r": {Owner: "o", Name: "r", FullName: "o/r"}}

	outcomes := Run(context.Background(), nil, config.RemediationConfig{Enabled: true}, results, rules, repos, slog.Default())
	if r.calls != 1 {
		t.Errorf("expected remediator to be called once, got %d", r.calls)
	}
	if len(outcomes) != 1 || outcomes[0].Err != nil || outcomes[0].PRURL != "" {
		t.Errorf("expected one no-op outcome (nil fix), got %+v", outcomes)
	}
}
