package fixes

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/compliance/checks"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
)

const workflowMissingHardenRunner = `name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@1111111111111111111111111111111111111111
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: step-security/harden-runner@2222222222222222222222222222222222222222
      - uses: actions/checkout@1111111111111111111111111111111111111111
`

func TestHardenRunnerFixer_InsertsFirstStepForMissingJobOnly(t *testing.T) {
	mux := http.NewServeMux()
	serveWorkflowFile(mux, "o", "scanned-repo", workflowMissingHardenRunner)
	mux.HandleFunc("/api/v3/repos/step-security/harden-runner/commits/v2", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("3333333333333333333333333333333333333333"))
	})
	client := newTestClient(t, mux)

	repo := gh.Repository{Owner: "o", Name: "scanned-repo", FullName: "o/scanned-repo", DefaultBranch: "main"}
	rule := config.Rule{ID: "harden-runner-required"}
	result := compliance.Result{RuleID: "harden-runner-required", Repo: "o/scanned-repo", Status: compliance.StatusFail}

	f := &hardenRunnerFixer{}
	fix, err := f.Remediate(context.Background(), client, repo, rule, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fix == nil || len(fix.Files) != 1 {
		t.Fatalf("expected exactly one file change, got %+v", fix)
	}

	got := string(fix.Files[0].Content)
	if !strings.Contains(got, "step-security/harden-runner@3333333333333333333333333333333333333333 # v2") {
		t.Errorf("expected pinned harden-runner step, got:\n%s", got)
	}

	// The `build` job's inserted step must precede its existing checkout step.
	if idx := strings.Index(got, "  build:"); idx == -1 {
		t.Fatal("build job missing from output")
	}
	buildSection := got[strings.Index(got, "  build:"):strings.Index(got, "  test:")]
	if strings.Index(buildSection, "harden-runner") > strings.Index(buildSection, "actions/checkout") {
		t.Errorf("harden-runner step should precede the existing checkout step in build job, got:\n%s", buildSection)
	}

	// The `test` job already has harden-runner as its first step and must be left untouched.
	if strings.Count(got, "step-security/harden-runner") != 2 {
		t.Errorf("expected exactly 2 harden-runner references (1 pre-existing + 1 inserted), got:\n%s", got)
	}

	// Sanity: the checker no longer finds any missing jobs in the fixed content.
	if missing := checks.FindJobsMissingHardenRunner(got); len(missing) != 0 {
		t.Errorf("expected no jobs missing harden-runner after fix, got %+v", missing)
	}
}

func TestHardenRunnerFixer_ReturnsNilWhenAllJobsCompliant(t *testing.T) {
	compliant := `name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: step-security/harden-runner@2222222222222222222222222222222222222222
      - uses: actions/checkout@1111111111111111111111111111111111111111
`
	mux := http.NewServeMux()
	serveWorkflowFile(mux, "o", "scanned-repo", compliant)
	client := newTestClient(t, mux)

	repo := gh.Repository{Owner: "o", Name: "scanned-repo", FullName: "o/scanned-repo", DefaultBranch: "main"}
	rule := config.Rule{ID: "harden-runner-required"}
	result := compliance.Result{RuleID: "harden-runner-required", Repo: "o/scanned-repo", Status: compliance.StatusFail}

	f := &hardenRunnerFixer{}
	fix, err := f.Remediate(context.Background(), client, repo, rule, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fix != nil {
		t.Errorf("expected nil fix when all jobs already compliant, got %+v", fix)
	}
}
