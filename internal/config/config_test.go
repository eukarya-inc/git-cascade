package config

import (
	"testing"
)

func boolPtr(b bool) *bool { return &b }

func TestParseWithScope(t *testing.T) {
	yaml := `
version: "1"
scope:
  include_public: false
  include_private: true
  include_archived: false
  exclude_repos:
    - sandbox
    - test-repo
rules:
  - id: test-rule
    name: Test Rule
    description: A test rule
    severity: warning
    enabled: true
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Scope.IncludePublic == nil || *cfg.Scope.IncludePublic != false {
		t.Error("expected include_public=false")
	}
	if cfg.Scope.IncludePrivate == nil || *cfg.Scope.IncludePrivate != true {
		t.Error("expected include_private=true")
	}
	if cfg.Scope.IncludeArchived == nil || *cfg.Scope.IncludeArchived != false {
		t.Error("expected include_archived=false")
	}
	if len(cfg.Scope.ExcludeRepos) != 2 || cfg.Scope.ExcludeRepos[0] != "sandbox" {
		t.Errorf("unexpected exclude_repos: %v", cfg.Scope.ExcludeRepos)
	}
}

func TestParseWithoutScope(t *testing.T) {
	yaml := `
version: "1"
rules:
  - id: test-rule
    name: Test Rule
    description: A test rule
    severity: warning
    enabled: true
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// All scope fields should be nil/empty
	if cfg.Scope.IncludePublic != nil {
		t.Error("expected include_public=nil when omitted")
	}
	if cfg.Scope.IncludePrivate != nil {
		t.Error("expected include_private=nil when omitted")
	}
	if len(cfg.Scope.IncludeRepos) != 0 {
		t.Error("expected empty include_repos when omitted")
	}
}

func TestScopeMerge(t *testing.T) {
	base := Scope{
		IncludePublic:  boolPtr(true),
		IncludePrivate: boolPtr(true),
		ExcludeRepos:   []string{"sandbox"},
	}
	other := Scope{
		IncludePublic: boolPtr(false),
		ExcludeRepos:  []string{"archive"},
	}

	merged := base.Merge(other)

	// IncludePublic overridden by other
	if *merged.IncludePublic != false {
		t.Error("expected IncludePublic=false after merge")
	}
	// IncludePrivate kept from base (other has nil)
	if *merged.IncludePrivate != true {
		t.Error("expected IncludePrivate=true from base")
	}
	// IncludeArchived still nil (neither set it)
	if merged.IncludeArchived != nil {
		t.Error("expected IncludeArchived=nil when neither set")
	}
	// ExcludeRepos appended
	if len(merged.ExcludeRepos) != 2 || merged.ExcludeRepos[0] != "sandbox" || merged.ExcludeRepos[1] != "archive" {
		t.Errorf("unexpected ExcludeRepos: %v", merged.ExcludeRepos)
	}
}

func TestScopeMergeIncludeRepos(t *testing.T) {
	base := Scope{
		IncludeRepos: []string{"api"},
	}
	other := Scope{
		IncludeRepos: []string{"web"},
	}

	merged := base.Merge(other)
	if len(merged.IncludeRepos) != 2 || merged.IncludeRepos[0] != "api" || merged.IncludeRepos[1] != "web" {
		t.Errorf("expected IncludeRepos=[api, web], got %v", merged.IncludeRepos)
	}
}

// — Rule.UnmarshalYAML (params with list values) ——————————————————————————————

func TestParseRule_ScalarParams(t *testing.T) {
	yaml := `
version: "1"
rules:
  - id: branch-protection
    name: Branch Protection
    severity: error
    enabled: true
    params:
      require_reviews: "true"
      required_reviewers: "2"
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rule := cfg.Rules[0]
	if rule.Params["require_reviews"] != "true" {
		t.Errorf("expected require_reviews=true, got %q", rule.Params["require_reviews"])
	}
	if rule.Params["required_reviewers"] != "2" {
		t.Errorf("expected required_reviewers=2, got %q", rule.Params["required_reviewers"])
	}
	if len(rule.ListParams) != 0 {
		t.Errorf("expected no ListParams, got %v", rule.ListParams)
	}
}

func TestParseRule_ListParams(t *testing.T) {
	yaml := `
version: "1"
rules:
  - id: branch-protection
    name: Branch Protection
    severity: error
    enabled: true
    params:
      additional_branches:
        - develop
        - staging
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rule := cfg.Rules[0]
	branches := rule.ListParams["additional_branches"]
	if len(branches) != 2 || branches[0] != "develop" || branches[1] != "staging" {
		t.Errorf("unexpected additional_branches: %v", branches)
	}
	if len(rule.Params) != 0 {
		t.Errorf("expected no scalar Params, got %v", rule.Params)
	}
}

func TestParseRule_MixedParams(t *testing.T) {
	yaml := `
version: "1"
rules:
  - id: branch-protection
    name: Branch Protection
    severity: error
    enabled: true
    params:
      require_reviews: "true"
      required_reviewers: "1"
      additional_branches:
        - develop
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rule := cfg.Rules[0]
	if rule.Params["require_reviews"] != "true" {
		t.Errorf("expected require_reviews=true, got %q", rule.Params["require_reviews"])
	}
	branches := rule.ListParams["additional_branches"]
	if len(branches) != 1 || branches[0] != "develop" {
		t.Errorf("unexpected additional_branches: %v", branches)
	}
}

func TestParseRule_NoParams(t *testing.T) {
	yaml := `
version: "1"
rules:
  - id: actions-pinned
    name: Actions Pinned
    severity: error
    enabled: true
`
	cfg, err := Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rule := cfg.Rules[0]
	if len(rule.Params) != 0 {
		t.Errorf("expected no Params, got %v", rule.Params)
	}
	if len(rule.ListParams) != 0 {
		t.Errorf("expected no ListParams, got %v", rule.ListParams)
	}
}

func TestBoolDefault(t *testing.T) {
	if BoolDefault(nil, true) != true {
		t.Error("expected true for nil with default true")
	}
	if BoolDefault(nil, false) != false {
		t.Error("expected false for nil with default false")
	}
	if BoolDefault(boolPtr(false), true) != false {
		t.Error("expected false for explicit false")
	}
	if BoolDefault(boolPtr(true), false) != true {
		t.Error("expected true for explicit true")
	}
}
