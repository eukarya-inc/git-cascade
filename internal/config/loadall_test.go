package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

func TestLoadAll_SingleFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "base.yaml", `
version: "1"
rules:
  - id: readme-exists
    name: README
    description: desc
    severity: warning
    enabled: true
`)
	cfg, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Version != "1" {
		t.Errorf("expected version=1, got %q", cfg.Version)
	}
	if len(cfg.Rules) != 1 || cfg.Rules[0].ID != "readme-exists" {
		t.Errorf("unexpected rules: %v", cfg.Rules)
	}
}

func TestLoadAll_MultiFile_RulesAppended(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "base.yaml", `
version: "1"
rules:
  - id: readme-exists
    name: README
    description: desc
    severity: warning
    enabled: true
`)
	writeFile(t, dir, "security.yaml", `
rules:
  - id: branch-protection
    name: Branch Protection
    description: desc
    severity: error
    enabled: true
`)
	cfg, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(cfg.Rules))
	}
	ids := map[string]bool{cfg.Rules[0].ID: true, cfg.Rules[1].ID: true}
	if !ids["readme-exists"] || !ids["branch-protection"] {
		t.Errorf("unexpected rule IDs: %v", ids)
	}
}

func TestLoadAll_VersionOnlyInOneFile(t *testing.T) {
	dir := t.TempDir()
	// base.yaml has version; security.yaml does not
	writeFile(t, dir, "base.yaml", `
version: "1"
rules:
  - id: readme-exists
    name: README
    description: desc
    severity: warning
    enabled: true
`)
	writeFile(t, dir, "security.yaml", `
rules:
  - id: branch-protection
    name: Branch Protection
    description: desc
    severity: error
    enabled: true
`)
	cfg, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("expected no error for missing version in partial file: %v", err)
	}
	if cfg.Version != "1" {
		t.Errorf("expected version=1, got %q", cfg.Version)
	}
}

func TestLoadAll_NoVersionAnywhere_Fails(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "rules.yaml", `
rules:
  - id: readme-exists
    name: README
    description: desc
    severity: warning
    enabled: true
`)
	_, err := LoadAll(dir)
	if err == nil {
		t.Fatal("expected error when no file has version")
	}
}

func TestLoadAll_SkipsNonYAML(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "base.yaml", `
version: "1"
rules:
  - id: readme-exists
    name: README
    description: desc
    severity: warning
    enabled: true
`)
	writeFile(t, dir, "README.md", "# not a config")
	writeFile(t, dir, "notes.txt", "some notes")
	cfg, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(cfg.Rules))
	}
}

func TestLoadAll_OutputFirstWins(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.yaml", `
version: "1"
output:
  format: json
rules:
  - id: readme-exists
    name: README
    description: desc
    severity: warning
    enabled: true
`)
	writeFile(t, dir, "b.yaml", `
output:
  format: csv
rules:
  - id: branch-protection
    name: Branch Protection
    description: desc
    severity: error
    enabled: true
`)
	cfg, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Output.Format != "json" {
		t.Errorf("expected output.format=json (first wins), got %q", cfg.Output.Format)
	}
}

func TestLoadAll_ScopeExcludeReposAppended(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.yaml", `
version: "1"
scope:
  exclude_repos:
    - sandbox
rules:
  - id: readme-exists
    name: README
    description: desc
    severity: warning
    enabled: true
`)
	writeFile(t, dir, "b.yaml", `
scope:
  exclude_repos:
    - archive
rules:
  - id: branch-protection
    name: Branch Protection
    description: desc
    severity: error
    enabled: true
`)
	cfg, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Scope.ExcludeRepos) != 2 {
		t.Errorf("expected 2 exclude_repos, got %v", cfg.Scope.ExcludeRepos)
	}
}

func TestLoadAll_DuplicateRuleIDs_Fails(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.yaml", `
version: "1"
rules:
  - id: readme-exists
    name: README
    description: desc
    severity: warning
    enabled: true
`)
	writeFile(t, dir, "b.yaml", `
rules:
  - id: readme-exists
    name: README duplicate
    description: desc
    severity: error
    enabled: true
`)
	_, err := LoadAll(dir)
	if err == nil {
		t.Fatal("expected error for duplicate rule IDs across files")
	}
}
