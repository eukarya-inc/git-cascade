package checks

import (
	"context"
	"strings"
	"testing"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
)

// ——— nodejsInstallLockChecker.Check ——————————————————————————————————————————

func TestNodejsInstallLockChecker_NoWorkflowsDir(t *testing.T) {
	fake := newFakeGitHub()
	// No .github/workflows registered → 404 from the fake server.
	_, client := fake.serve(t)

	c := &nodejsInstallLockChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("npm-ci-required"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusSkip {
		t.Errorf("expected skip, got %s: %s", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, "no .github/workflows directory") {
		t.Errorf("unexpected skip message: %q", result.Message)
	}
}

func TestNodejsInstallLockChecker_NoNodeInstallCommands(t *testing.T) {
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

	c := &nodejsInstallLockChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("npm-ci-required"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusSkip {
		t.Errorf("expected skip when no Node.js install commands, got %s: %s", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, "no Node.js install commands found") {
		t.Errorf("unexpected skip message: %q", result.Message)
	}
}

func TestNodejsInstallLockChecker_NpmInstall_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setDir("org", "repo", ".github/workflows", []string{"ci.yml"})
	fake.setFile("org", "repo", ".github/workflows/ci.yml", []byte(`
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: npm install
`))
	_, client := fake.serve(t)

	c := &nodejsInstallLockChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("npm-ci-required"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail for npm install, got %s: %s", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, "ci.yml") {
		t.Errorf("expected message to name the file, got %q", result.Message)
	}
	if !strings.Contains(result.Message, "npm install") {
		t.Errorf("expected message to contain reason, got %q", result.Message)
	}
}

func TestNodejsInstallLockChecker_NpmCi_Pass(t *testing.T) {
	fake := newFakeGitHub()
	fake.setDir("org", "repo", ".github/workflows", []string{"ci.yml"})
	fake.setFile("org", "repo", ".github/workflows/ci.yml", []byte(`
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: npm ci
`))
	_, client := fake.serve(t)

	c := &nodejsInstallLockChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("npm-ci-required"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass for npm ci, got %s: %s", result.Status, result.Message)
	}
}

func TestNodejsInstallLockChecker_PnpmInstallWithoutFrozenLockfile_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setDir("org", "repo", ".github/workflows", []string{"ci.yml"})
	fake.setFile("org", "repo", ".github/workflows/ci.yml", []byte(`
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: pnpm install
`))
	_, client := fake.serve(t)

	c := &nodejsInstallLockChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("npm-ci-required"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail for pnpm install without --frozen-lockfile, got %s: %s", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, "pnpm install without --frozen-lockfile") {
		t.Errorf("unexpected failure message: %q", result.Message)
	}
}

func TestNodejsInstallLockChecker_PnpmInstallFrozenLockfile_Pass(t *testing.T) {
	fake := newFakeGitHub()
	fake.setDir("org", "repo", ".github/workflows", []string{"ci.yml"})
	fake.setFile("org", "repo", ".github/workflows/ci.yml", []byte(`
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: pnpm install --frozen-lockfile
`))
	_, client := fake.serve(t)

	c := &nodejsInstallLockChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("npm-ci-required"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass for pnpm install --frozen-lockfile, got %s: %s", result.Status, result.Message)
	}
}

func TestNodejsInstallLockChecker_YarnInstallWithoutImmutable_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setDir("org", "repo", ".github/workflows", []string{"ci.yml"})
	fake.setFile("org", "repo", ".github/workflows/ci.yml", []byte(`
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: yarn install
`))
	_, client := fake.serve(t)

	c := &nodejsInstallLockChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("npm-ci-required"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail for yarn install without --immutable, got %s: %s", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, "yarn install without --frozen-lockfile/--immutable") {
		t.Errorf("unexpected failure message: %q", result.Message)
	}
}

func TestNodejsInstallLockChecker_YarnInstallImmutable_Pass(t *testing.T) {
	fake := newFakeGitHub()
	fake.setDir("org", "repo", ".github/workflows", []string{"ci.yml"})
	fake.setFile("org", "repo", ".github/workflows/ci.yml", []byte(`
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: yarn install --immutable
`))
	_, client := fake.serve(t)

	c := &nodejsInstallLockChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("npm-ci-required"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass for yarn install --immutable, got %s: %s", result.Status, result.Message)
	}
}

func TestNodejsInstallLockChecker_YarnVersionOnly_Skip(t *testing.T) {
	// A workflow whose only yarn usage is `yarn --version` performs no install,
	// so the check is not applicable and must skip (not pass, not fail).
	fake := newFakeGitHub()
	fake.setDir("org", "repo", ".github/workflows", []string{"ci.yml"})
	fake.setFile("org", "repo", ".github/workflows/ci.yml", []byte(`
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: |
          corepack enable
          yarn --version
`))
	_, client := fake.serve(t)

	c := &nodejsInstallLockChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("npm-ci-required"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusSkip {
		t.Errorf("expected skip when only `yarn --version` is present, got %s: %s", result.Status, result.Message)
	}
}

func TestNodejsInstallLockChecker_YarnVersionWithImmutableInstall_Pass(t *testing.T) {
	// Regression: a diagnostic `yarn --version` step alongside a locked
	// `yarn install --immutable` must not be misreported as an unlocked install.
	fake := newFakeGitHub()
	fake.setDir("org", "repo", ".github/workflows", []string{"ci.yml"})
	fake.setFile("org", "repo", ".github/workflows/ci.yml", []byte(`
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: |
          corepack enable
          yarn --version
      - run: yarn install --immutable
`))
	_, client := fake.serve(t)

	c := &nodejsInstallLockChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("npm-ci-required"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass when `yarn --version` accompanies a locked install, got %s: %s", result.Status, result.Message)
	}
}

func TestNodejsInstallLockChecker_BareYarnImmutable_Pass(t *testing.T) {
	// `yarn --immutable` (bare Yarn Berry install with the lock flag) is a valid
	// locked install and must pass.
	fake := newFakeGitHub()
	fake.setDir("org", "repo", ".github/workflows", []string{"ci.yml"})
	fake.setFile("org", "repo", ".github/workflows/ci.yml", []byte(`
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: yarn --immutable
`))
	_, client := fake.serve(t)

	c := &nodejsInstallLockChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("npm-ci-required"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass for `yarn --immutable`, got %s: %s", result.Status, result.Message)
	}
}

func TestNodejsInstallLockChecker_NonYamlFileIgnored(t *testing.T) {
	fake := newFakeGitHub()
	// Directory contains a README (no .yml/.yaml extension) — must be ignored.
	fake.setDir("org", "repo", ".github/workflows", []string{"README.md"})
	fake.setFile("org", "repo", ".github/workflows/README.md", []byte(`npm install`))
	_, client := fake.serve(t)

	c := &nodejsInstallLockChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("npm-ci-required"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	// Non-yaml file is ignored, so no install commands are detected → skip.
	if result.Status != compliance.StatusSkip {
		t.Errorf("expected skip when only non-yaml files present, got %s: %s", result.Status, result.Message)
	}
}

func TestNodejsInstallLockChecker_MultipleFiles_OneViolation(t *testing.T) {
	fake := newFakeGitHub()
	fake.setDir("org", "repo", ".github/workflows", []string{"ci.yml", "release.yml"})
	// ci.yml uses npm ci (compliant).
	fake.setFile("org", "repo", ".github/workflows/ci.yml", []byte(`
on: [push]
jobs:
  build:
    steps:
      - run: npm ci
`))
	// release.yml uses bare npm install (violation).
	fake.setFile("org", "repo", ".github/workflows/release.yml", []byte(`
on: [push]
jobs:
  release:
    steps:
      - run: npm install
`))
	_, client := fake.serve(t)

	c := &nodejsInstallLockChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("npm-ci-required"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail when one of two files has a violation, got %s: %s", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, "release.yml") {
		t.Errorf("expected failure message to name the violating file, got %q", result.Message)
	}
	// The compliant file must not appear in the failure message.
	if strings.Contains(result.Message, "ci.yml") {
		t.Errorf("compliant file should not appear in failure message, got %q", result.Message)
	}
}

func TestNodejsInstallLockChecker_AllowComment(t *testing.T) {
	// An npm install line annotated with # git-cascade:allow must not produce a violation.
	fake := newFakeGitHub()
	fake.setDir("org", "repo", ".github/workflows", []string{"ci.yml"})
	fake.setFile("org", "repo", ".github/workflows/ci.yml", []byte(`
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: npm install # git-cascade:allow
`))
	_, client := fake.serve(t)

	c := &nodejsInstallLockChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("npm-ci-required"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass with allow comment, got %s: %s", result.Status, result.Message)
	}
}
