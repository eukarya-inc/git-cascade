package config

import (
	"strings"
	"testing"
)

func TestValidate_MissingVersion(t *testing.T) {
	cfg := &ComplianceConfig{}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "version") {
		t.Errorf("expected version error, got %v", err)
	}
}

func TestValidate_MissingRuleID(t *testing.T) {
	cfg := &ComplianceConfig{
		Version: "1",
		Rules:   []Rule{{Name: "no id", Severity: SeverityError}},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "missing an id") {
		t.Errorf("expected missing id error, got %v", err)
	}
}

func TestValidate_DuplicateRuleID(t *testing.T) {
	cfg := &ComplianceConfig{
		Version: "1",
		Rules: []Rule{
			{ID: "dup", Name: "A", Severity: SeverityWarning},
			{ID: "dup", Name: "B", Severity: SeverityWarning},
		},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("expected duplicate error, got %v", err)
	}
}

func TestValidate_InvalidSeverity(t *testing.T) {
	cfg := &ComplianceConfig{
		Version: "1",
		Rules:   []Rule{{ID: "r1", Name: "R", Severity: "critical"}},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "invalid severity") {
		t.Errorf("expected severity error, got %v", err)
	}
}

func TestValidate_AllSeverities(t *testing.T) {
	for _, s := range []Severity{SeverityError, SeverityWarning, SeverityInfo} {
		cfg := &ComplianceConfig{
			Version: "1",
			Rules:   []Rule{{ID: "r1", Name: "R", Severity: s}},
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("severity %q should be valid, got %v", s, err)
		}
	}
}

func TestParsePartial_NoVersionAllowed(t *testing.T) {
	data := []byte(`
rules:
  - id: readme-exists
    name: README
    description: desc
    severity: warning
    enabled: true
`)
	cfg, err := ParsePartial(data)
	if err != nil {
		t.Fatalf("ParsePartial should not fail without version: %v", err)
	}
	if len(cfg.Rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(cfg.Rules))
	}
}
