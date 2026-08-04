package fixes

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
)

func serveWorkflowFile(mux *http.ServeMux, owner, repo, content string) {
	mux.HandleFunc("/api/v3/repos/"+owner+"/"+repo+"/contents/.github/workflows", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []map[string]any{{"type": "file", "name": "ci.yml", "path": ".github/workflows/ci.yml"}})
	})
	mux.HandleFunc("/api/v3/repos/"+owner+"/"+repo+"/contents/.github/workflows/ci.yml", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"type":     "file",
			"name":     "ci.yml",
			"path":     ".github/workflows/ci.yml",
			"encoding": "base64",
			"content":  base64.StdEncoding.EncodeToString([]byte(content)),
		})
	})
}

const workflowWithUnlockedInstalls = `name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: npm install
      - run: pnpm install
      - run: yarn
      - run: npm install left-pad
      - run: npm install && npm test
`

func TestNpmCiRequiredFixer_LocksBareInstallsAndSkipsUnsafeOnes(t *testing.T) {
	mux := http.NewServeMux()
	serveWorkflowFile(mux, "o", "scanned-repo", workflowWithUnlockedInstalls)
	client := newTestClient(t, mux)

	repo := gh.Repository{Owner: "o", Name: "scanned-repo", FullName: "o/scanned-repo", DefaultBranch: "main"}
	rule := config.Rule{ID: "npm-ci-required"}
	result := compliance.Result{RuleID: "npm-ci-required", Repo: "o/scanned-repo", Status: compliance.StatusFail}

	f := &npmCiRequiredFixer{}
	fix, err := f.Remediate(context.Background(), client, repo, rule, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fix == nil || len(fix.Files) != 1 {
		t.Fatalf("expected exactly one file change, got %+v", fix)
	}

	got := string(fix.Files[0].Content)
	if !strings.Contains(got, "run: npm ci") {
		t.Errorf("expected bare npm install to become npm ci, got:\n%s", got)
	}
	if !strings.Contains(got, "run: pnpm install --frozen-lockfile") {
		t.Errorf("expected pnpm install to gain --frozen-lockfile, got:\n%s", got)
	}
	if !strings.Contains(got, "run: yarn --immutable") {
		t.Errorf("expected bare yarn to gain --immutable, got:\n%s", got)
	}
	if !strings.Contains(got, "run: npm install left-pad") {
		t.Errorf("npm install <pkg> should be left untouched (not a bare restore), got:\n%s", got)
	}
	if !strings.Contains(got, "run: npm install && npm test") {
		t.Errorf("compound command should be left untouched, got:\n%s", got)
	}
}

func TestNpmCiRequiredFixer_ReturnsNilWhenAllLocked(t *testing.T) {
	mux := http.NewServeMux()
	serveWorkflowFile(mux, "o", "scanned-repo", "name: CI\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: npm ci\n")
	client := newTestClient(t, mux)

	repo := gh.Repository{Owner: "o", Name: "scanned-repo", FullName: "o/scanned-repo", DefaultBranch: "main"}
	rule := config.Rule{ID: "npm-ci-required"}
	result := compliance.Result{RuleID: "npm-ci-required", Repo: "o/scanned-repo", Status: compliance.StatusFail}

	f := &npmCiRequiredFixer{}
	fix, err := f.Remediate(context.Background(), client, repo, rule, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fix != nil {
		t.Errorf("expected nil fix when nothing to lock, got %+v", fix)
	}
}
