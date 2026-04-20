package checks

import (
	"context"
	"testing"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
)

// baseRule returns a minimal enabled Rule for tests.
func baseRule(id string) config.Rule {
	return config.Rule{
		ID:       id,
		Name:     id,
		Severity: config.SeverityWarning,
		Enabled:  true,
	}
}

// pubRepo returns a public repository fixture.
func pubRepo() gh.Repository {
	return gh.Repository{
		Owner:         "org",
		Name:          "repo",
		FullName:      "org/repo",
		DefaultBranch: "main",
		Private:       false,
	}
}

// privRepo returns a private repository fixture.
func privRepo() gh.Repository {
	return gh.Repository{
		Owner:         "org",
		Name:          "repo",
		FullName:      "org/repo",
		DefaultBranch: "main",
		Private:       true,
	}
}

// ——— codeownersChecker ————————————————————————————————————————————————————————

func TestCodeownersChecker_Found(t *testing.T) {
	fake := newFakeGitHub()
	fake.setFile("org", "repo", ".github/CODEOWNERS", []byte("* @team\n"))
	_, client := fake.serve(t)

	c := &codeownersChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("codeowners-exists"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass, got %s: %s", result.Status, result.Message)
	}
}

func TestCodeownersChecker_RootFallback(t *testing.T) {
	fake := newFakeGitHub()
	// Only the root-level CODEOWNERS exists.
	fake.setFile("org", "repo", "CODEOWNERS", []byte("* @team\n"))
	_, client := fake.serve(t)

	c := &codeownersChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("codeowners-exists"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass, got %s", result.Status)
	}
}

func TestCodeownersChecker_Missing(t *testing.T) {
	fake := newFakeGitHub()
	_, client := fake.serve(t)

	c := &codeownersChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("codeowners-exists"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s", result.Status)
	}
}

// ——— fileExistsChecker (readme-exists / license-exists) ——————————————————————

func TestFileExistsChecker_Found(t *testing.T) {
	fake := newFakeGitHub()
	fake.setFile("org", "repo", "README.md", []byte("# Hello\n"))
	_, client := fake.serve(t)

	c := &fileExistsChecker{id: "readme-exists", files: []string{"README.md", "README"}}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("readme-exists"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass, got %s", result.Status)
	}
}

func TestFileExistsChecker_SecondCandidate(t *testing.T) {
	fake := newFakeGitHub()
	// Only the second candidate exists.
	fake.setFile("org", "repo", "README", []byte("Hello\n"))
	_, client := fake.serve(t)

	c := &fileExistsChecker{id: "readme-exists", files: []string{"README.md", "README"}}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("readme-exists"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass (second candidate), got %s", result.Status)
	}
}

func TestFileExistsChecker_Missing(t *testing.T) {
	fake := newFakeGitHub()
	_, client := fake.serve(t)

	c := &fileExistsChecker{id: "readme-exists", files: []string{"README.md", "README"}}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("readme-exists"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s", result.Status)
	}
}

// ——— lockfileRequiredChecker —————————————————————————————————————————————————

func TestLockfileRequiredChecker_AllPresent(t *testing.T) {
	fake := newFakeGitHub()
	fake.setFile("org", "repo", "go.mod", []byte("module x\n"))
	fake.setFile("org", "repo", "go.sum", []byte(""))
	_, client := fake.serve(t)

	c := &lockfileRequiredChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("lockfile-required"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass, got %s: %s", result.Status, result.Message)
	}
}

func TestLockfileRequiredChecker_MissingLockfile(t *testing.T) {
	fake := newFakeGitHub()
	fake.setFile("org", "repo", "go.mod", []byte("module x\n"))
	// No go.sum → violation.
	_, client := fake.serve(t)

	c := &lockfileRequiredChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("lockfile-required"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s", result.Status)
	}
}

func TestLockfileRequiredChecker_NoManifests(t *testing.T) {
	fake := newFakeGitHub()
	_, client := fake.serve(t)

	c := &lockfileRequiredChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("lockfile-required"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	// No manifests at all → pass (nothing to check).
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass for no manifests, got %s", result.Status)
	}
}

// ——— noEnvFilesChecker ———————————————————————————————————————————————————————

func TestNoEnvFilesChecker_NoEnvFiles(t *testing.T) {
	fake := newFakeGitHub()
	fake.setDir("org", "repo", "", []string{"README.md", "go.mod"})
	_, client := fake.serve(t)

	c := &noEnvFilesChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("no-env-files"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass, got %s: %s", result.Status, result.Message)
	}
}

func TestNoEnvFilesChecker_EnvFileFound(t *testing.T) {
	fake := newFakeGitHub()
	fake.setDir("org", "repo", "", []string{"README.md", ".env", "go.mod"})
	_, client := fake.serve(t)

	c := &noEnvFilesChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("no-env-files"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s", result.Status)
	}
}

func TestNoEnvFilesChecker_ExampleAllowed(t *testing.T) {
	fake := newFakeGitHub()
	fake.setDir("org", "repo", "", []string{".env.example"})
	_, client := fake.serve(t)

	c := &noEnvFilesChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("no-env-files"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass for .env.example, got %s", result.Status)
	}
}

// ——— pullRequestTargetChecker —————————————————————————————————————————————————

func TestPRTargetChecker_NoWorkflowsDir(t *testing.T) {
	fake := newFakeGitHub()
	_, client := fake.serve(t)

	c := &pullRequestTargetChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("no-pull-request-target"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusSkip {
		t.Errorf("expected skip for missing workflows dir, got %s", result.Status)
	}
}

func TestPRTargetChecker_NoViolation(t *testing.T) {
	fake := newFakeGitHub()
	fake.setDir("org", "repo", ".github/workflows", []string{"ci.yml"})
	fake.setFile("org", "repo", ".github/workflows/ci.yml", []byte(`
on:
  push:
    branches: [main]
jobs:
  build:
    runs-on: ubuntu-latest
`))
	_, client := fake.serve(t)

	c := &pullRequestTargetChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("no-pull-request-target"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass, got %s: %s", result.Status, result.Message)
	}
}

func TestPRTargetChecker_Violation(t *testing.T) {
	fake := newFakeGitHub()
	fake.setDir("org", "repo", ".github/workflows", []string{"ci.yml"})
	fake.setFile("org", "repo", ".github/workflows/ci.yml", []byte(`
on:
  pull_request_target:
    types: [opened]
`))
	_, client := fake.serve(t)

	c := &pullRequestTargetChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("no-pull-request-target"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s", result.Status)
	}
}

// ——— secretsInheritChecker ————————————————————————————————————————————————————

func TestSecretsInheritChecker_NoWorkflowsDir(t *testing.T) {
	fake := newFakeGitHub()
	_, client := fake.serve(t)

	c := &secretsInheritChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("no-secrets-inherit"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusSkip {
		t.Errorf("expected skip, got %s", result.Status)
	}
}

func TestSecretsInheritChecker_Violation(t *testing.T) {
	fake := newFakeGitHub()
	fake.setDir("org", "repo", ".github/workflows", []string{"ci.yml"})
	fake.setFile("org", "repo", ".github/workflows/ci.yml", []byte(`
jobs:
  call:
    uses: org/repo/.github/workflows/reusable.yml@main
    secrets: inherit
`))
	_, client := fake.serve(t)

	c := &secretsInheritChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("no-secrets-inherit"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s: %s", result.Status, result.Message)
	}
}

func TestSecretsInheritChecker_Pass(t *testing.T) {
	fake := newFakeGitHub()
	fake.setDir("org", "repo", ".github/workflows", []string{"ci.yml"})
	fake.setFile("org", "repo", ".github/workflows/ci.yml", []byte(`
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hello
`))
	_, client := fake.serve(t)

	c := &secretsInheritChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("no-secrets-inherit"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass, got %s", result.Status)
	}
}

// ——— hardenRunnerChecker —————————————————————————————————————————————————————

func TestHardenRunnerChecker_PrivateRepo_Skip(t *testing.T) {
	fake := newFakeGitHub()
	_, client := fake.serve(t)

	c := &hardenRunnerChecker{}
	result, err := c.Check(context.Background(), client, privRepo(), baseRule("harden-runner-required"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusSkip {
		t.Errorf("expected skip for private repo, got %s", result.Status)
	}
}

func TestHardenRunnerChecker_NoWorkflowsDir(t *testing.T) {
	fake := newFakeGitHub()
	_, client := fake.serve(t)

	c := &hardenRunnerChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("harden-runner-required"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusSkip {
		t.Errorf("expected skip, got %s", result.Status)
	}
}

func TestHardenRunnerChecker_Pass(t *testing.T) {
	fake := newFakeGitHub()
	fake.setDir("org", "repo", ".github/workflows", []string{"ci.yml"})
	fake.setFile("org", "repo", ".github/workflows/ci.yml", []byte(`
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: step-security/harden-runner@abc123def456abc123def456abc123def456abc1
      - uses: actions/checkout@abc123
`))
	_, client := fake.serve(t)

	c := &hardenRunnerChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("harden-runner-required"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass, got %s: %s", result.Status, result.Message)
	}
}

func TestHardenRunnerChecker_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setDir("org", "repo", ".github/workflows", []string{"ci.yml"})
	fake.setFile("org", "repo", ".github/workflows/ci.yml", []byte(`
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@abc123
      - run: go build ./...
`))
	_, client := fake.serve(t)

	c := &hardenRunnerChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("harden-runner-required"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail, got %s", result.Status)
	}
}

// ——— actionsPinnedChecker ————————————————————————————————————————————————————

func TestActionsPinnedChecker_NoWorkflowsDir(t *testing.T) {
	fake := newFakeGitHub()
	_, client := fake.serve(t)

	c := &actionsPinnedChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("actions-pinned"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusSkip {
		t.Errorf("expected skip, got %s", result.Status)
	}
}

func TestActionsPinnedChecker_AllPinned(t *testing.T) {
	fake := newFakeGitHub()
	fake.setDir("org", "repo", ".github/workflows", []string{"ci.yml"})
	fake.setFile("org", "repo", ".github/workflows/ci.yml", []byte(`
jobs:
  build:
    steps:
      - uses: actions/checkout@abc123def456abc123def456abc123def456abc1
`))
	_, client := fake.serve(t)

	c := &actionsPinnedChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("actions-pinned"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass, got %s: %s", result.Status, result.Message)
	}
}

func TestActionsPinnedChecker_UnpinnedAction(t *testing.T) {
	fake := newFakeGitHub()
	fake.setDir("org", "repo", ".github/workflows", []string{"ci.yml"})
	fake.setFile("org", "repo", ".github/workflows/ci.yml", []byte(`
jobs:
  build:
    steps:
      - uses: actions/checkout@v4
`))
	_, client := fake.serve(t)

	c := &actionsPinnedChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("actions-pinned"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail for unpinned action, got %s", result.Status)
	}
}

// ——— dockerfileDigestChecker ——————————————————————————————————————————————————

func TestDockerfileDigestChecker_EmptyRepo(t *testing.T) {
	fake := newFakeGitHub()
	// No root dir listing → empty repo.
	_, client := fake.serve(t)

	c := &dockerfileDigestChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("dockerfile-digest"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusSkip {
		t.Errorf("expected skip for empty repo, got %s", result.Status)
	}
}

func TestDockerfileDigestChecker_NoDockerfile(t *testing.T) {
	fake := newFakeGitHub()
	fake.setDir("org", "repo", "", []string{"main.go", "go.mod"})
	_, client := fake.serve(t)

	c := &dockerfileDigestChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("dockerfile-digest"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusSkip {
		t.Errorf("expected skip for no Dockerfile, got %s", result.Status)
	}
}

func TestDockerfileDigestChecker_PinnedDigest(t *testing.T) {
	fake := newFakeGitHub()
	fake.setDir("org", "repo", "", []string{"Dockerfile"})
	fake.setFile("org", "repo", "Dockerfile", []byte(
		"FROM ubuntu@sha256:"+
			"a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2\n",
	))
	_, client := fake.serve(t)

	c := &dockerfileDigestChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("dockerfile-digest"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass, got %s: %s", result.Status, result.Message)
	}
}

func TestDockerfileDigestChecker_Unpinned(t *testing.T) {
	fake := newFakeGitHub()
	fake.setDir("org", "repo", "", []string{"Dockerfile"})
	fake.setFile("org", "repo", "Dockerfile", []byte("FROM ubuntu:22.04\n"))
	_, client := fake.serve(t)

	c := &dockerfileDigestChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("dockerfile-digest"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail for unpinned FROM, got %s", result.Status)
	}
}

// ——— externalCollaboratorsChecker ————————————————————————————————————————————

// externalCollaboratorsChecker calls gh.ListCollaborators which requires the
// /repos/{owner}/{repo}/collaborators endpoint. The fakeGitHub helper covers
// the contents API only; testing this checker end-to-end would require an
// additional endpoint. We cover the pure-logic branches via unit tests instead.

func TestExternalCollaboratorsChecker_ID(t *testing.T) {
	c := &externalCollaboratorsChecker{}
	if c.ID() != "external-collaborators" {
		t.Errorf("unexpected ID: %q", c.ID())
	}
}

// ——— aiConfigSafetyChecker ————————————————————————————————————————————————————

func TestAIConfigSafetyChecker_NoConfigs(t *testing.T) {
	fake := newFakeGitHub()
	// No .claude dir, no .mcp.json.
	_, client := fake.serve(t)

	c := &aiConfigSafetyChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("ai-config-safety"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass when no AI configs present, got %s", result.Status)
	}
}

func TestAIConfigSafetyChecker_SafeMCPJson(t *testing.T) {
	fake := newFakeGitHub()
	fake.setFile("org", "repo", ".mcp.json", []byte(`{"version": "1.0"}`))
	_, client := fake.serve(t)

	c := &aiConfigSafetyChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("ai-config-safety"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass for safe .mcp.json, got %s: %s", result.Status, result.Message)
	}
}

func TestAIConfigSafetyChecker_ViolationInMCPJson(t *testing.T) {
	fake := newFakeGitHub()
	fake.setFile("org", "repo", ".mcp.json", []byte(`{
		"mcpServers": {
			"myserver": {"command": "/usr/bin/myserver"}
		}
	}`))
	_, client := fake.serve(t)

	c := &aiConfigSafetyChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("ai-config-safety"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail for executable MCP server, got %s", result.Status)
	}
}

func TestAIConfigSafetyChecker_ViolationInClaudeDir(t *testing.T) {
	fake := newFakeGitHub()
	fake.setDir("org", "repo", ".claude", []string{"settings.json"})
	fake.setFile("org", "repo", ".claude/settings.json", []byte(`{
		"hooks": {
			"PostToolUse": [{"command": "echo hi"}]
		}
	}`))
	_, client := fake.serve(t)

	c := &aiConfigSafetyChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("ai-config-safety"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail for executable hook in .claude/, got %s", result.Status)
	}
}

func TestAIConfigSafetyChecker_SafeClaudeDir(t *testing.T) {
	fake := newFakeGitHub()
	fake.setDir("org", "repo", ".claude", []string{"settings.json"})
	fake.setFile("org", "repo", ".claude/settings.json", []byte(`{
		"theme": "dark"
	}`))
	_, client := fake.serve(t)

	c := &aiConfigSafetyChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("ai-config-safety"))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass for safe .claude/ config, got %s: %s", result.Status, result.Message)
	}
}
