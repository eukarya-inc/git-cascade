package config

import "testing"

func TestEffectiveAutoRemediation(t *testing.T) {
	cases := []struct {
		name string
		rule Rule
		want bool
	}{
		{"rule true", Rule{AutoRemediation: boolPtr(true)}, true},
		{"rule false", Rule{AutoRemediation: boolPtr(false)}, false},
		{"unset rule defaults to false", Rule{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := EffectiveAutoRemediation(c.rule)
			if got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestParseWithRemediation(t *testing.T) {
	yaml := `
version: "1"
remediation:
  enabled: true
  branch_prefix: "custom/fix"
  commit_author:
    name: bot
    email: bot@example.com
  pr_labels:
    - automated-fix
  draft_pr: true
rules:
  - id: actions-pinned
    name: Actions Pinned
    description: test
    severity: error
    enabled: true
    auto_remediation: true
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.Remediation.Enabled {
		t.Error("expected remediation.enabled=true")
	}
	if cfg.Remediation.BranchPrefix != "custom/fix" {
		t.Errorf("got branch_prefix=%q", cfg.Remediation.BranchPrefix)
	}
	if cfg.Remediation.CommitAuthor.Name != "bot" || cfg.Remediation.CommitAuthor.Email != "bot@example.com" {
		t.Errorf("got commit_author=%+v", cfg.Remediation.CommitAuthor)
	}
	if len(cfg.Remediation.PRLabels) != 1 || cfg.Remediation.PRLabels[0] != "automated-fix" {
		t.Errorf("got pr_labels=%v", cfg.Remediation.PRLabels)
	}
	if !cfg.Remediation.DraftPR {
		t.Error("expected draft_pr=true")
	}

	rule := cfg.Rules[0]
	if rule.AutoRemediation == nil || !*rule.AutoRemediation {
		t.Error("expected rule.auto_remediation=true")
	}
	if !EffectiveAutoRemediation(rule) {
		t.Error("expected EffectiveAutoRemediation to be true for this rule")
	}
}

func TestParseWithRemediation_RuleOptOutDefaultsFalse(t *testing.T) {
	yaml := `
version: "1"
remediation:
  enabled: true
rules:
  - id: actions-pinned
    name: Actions Pinned
    description: test
    severity: error
    enabled: true
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rule := cfg.Rules[0]
	if rule.AutoRemediation != nil {
		t.Errorf("expected auto_remediation to be unset, got %v", *rule.AutoRemediation)
	}
	if EffectiveAutoRemediation(rule) {
		t.Error("a rule with no auto_remediation field should default to false even when remediation.enabled is true")
	}
}
