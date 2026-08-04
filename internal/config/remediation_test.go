package config

import "testing"

func TestEffectiveAutoRemediation_RulePrecedesGlobalDefault(t *testing.T) {
	cases := []struct {
		name        string
		rule        Rule
		remediation RemediationConfig
		want        bool
	}{
		{"rule true overrides global false", Rule{AutoRemediation: boolPtr(true)}, RemediationConfig{AutoRemediation: boolPtr(false)}, true},
		{"rule false overrides global true", Rule{AutoRemediation: boolPtr(false)}, RemediationConfig{AutoRemediation: boolPtr(true)}, false},
		{"unset rule falls back to global true", Rule{}, RemediationConfig{AutoRemediation: boolPtr(true)}, true},
		{"unset rule and global defaults to false", Rule{}, RemediationConfig{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := EffectiveAutoRemediation(c.rule, c.remediation)
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
  auto_remediation: true
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
    auto_remediation: false
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !cfg.Remediation.Enabled {
		t.Error("expected remediation.enabled=true")
	}
	if cfg.Remediation.AutoRemediation == nil || !*cfg.Remediation.AutoRemediation {
		t.Error("expected remediation.auto_remediation=true")
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
	if rule.AutoRemediation == nil || *rule.AutoRemediation != false {
		t.Error("expected rule.auto_remediation=false")
	}
	if EffectiveAutoRemediation(rule, cfg.Remediation) != false {
		t.Error("rule-level auto_remediation=false should win over remediation.auto_remediation=true")
	}
}
