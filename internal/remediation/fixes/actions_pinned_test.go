package fixes

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/google/go-github/v84/github"
)

const workflowWithUnpinnedAction = `name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: ./.github/actions/local
`

func newTestClient(t *testing.T, mux *http.ServeMux) *github.Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client := github.NewClient(nil).WithAuthToken("fake-token")
	client, _ = client.WithEnterpriseURLs(srv.URL+"/", srv.URL+"/")
	return client
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func TestActionsPinnedFixer_RewritesUnpinnedActionAndSkipsLocal(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/o/scanned-repo/contents/.github/workflows", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []map[string]any{{"type": "file", "name": "ci.yml", "path": ".github/workflows/ci.yml"}})
	})
	mux.HandleFunc("/api/v3/repos/o/scanned-repo/contents/.github/workflows/ci.yml", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"type":     "file",
			"name":     "ci.yml",
			"path":     ".github/workflows/ci.yml",
			"encoding": "base64",
			"content":  base64.StdEncoding.EncodeToString([]byte(workflowWithUnpinnedAction)),
		})
	})
	// Resolves actions/checkout@v4 -> a full commit SHA (GetCommitSHA1 returns
	// the raw SHA as the response body, not JSON).
	mux.HandleFunc("/api/v3/repos/actions/checkout/commits/v4", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("1111111111111111111111111111111111111111"))
	})
	client := newTestClient(t, mux)

	repo := gh.Repository{Owner: "o", Name: "scanned-repo", FullName: "o/scanned-repo", DefaultBranch: "main"}
	rule := config.Rule{ID: "actions-pinned"}
	result := compliance.Result{RuleID: "actions-pinned", Repo: "o/scanned-repo", Status: compliance.StatusFail}

	f := &actionsPinnedFixer{}
	fix, err := f.Remediate(context.Background(), client, repo, rule, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fix == nil || len(fix.Files) != 1 {
		t.Fatalf("expected exactly one file change, got %+v", fix)
	}

	got := string(fix.Files[0].Content)
	if !strings.Contains(got, "actions/checkout@1111111111111111111111111111111111111111 # v4") {
		t.Errorf("expected pinned checkout line, got:\n%s", got)
	}
	if !strings.Contains(got, "./.github/actions/local") {
		t.Errorf("local action line should be preserved untouched, got:\n%s", got)
	}
	if fix.PRTitle == "" || fix.CommitMessage == "" {
		t.Error("expected non-empty PR title and commit message")
	}
}

func TestActionsPinnedFixer_ReturnsNilWhenRefUnresolvable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/o/scanned-repo/contents/.github/workflows", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []map[string]any{{"type": "file", "name": "ci.yml", "path": ".github/workflows/ci.yml"}})
	})
	mux.HandleFunc("/api/v3/repos/o/scanned-repo/contents/.github/workflows/ci.yml", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"type":     "file",
			"name":     "ci.yml",
			"path":     ".github/workflows/ci.yml",
			"encoding": "base64",
			"content":  base64.StdEncoding.EncodeToString([]byte(workflowWithUnpinnedAction)),
		})
	})
	// The tag can't be resolved (e.g. deleted) — checkout ref lookup 404s.
	mux.HandleFunc("/api/v3/repos/actions/checkout/commits/v4", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	client := newTestClient(t, mux)

	repo := gh.Repository{Owner: "o", Name: "scanned-repo", FullName: "o/scanned-repo", DefaultBranch: "main"}
	rule := config.Rule{ID: "actions-pinned"}
	result := compliance.Result{RuleID: "actions-pinned", Repo: "o/scanned-repo", Status: compliance.StatusFail}

	f := &actionsPinnedFixer{}
	fix, err := f.Remediate(context.Background(), client, repo, rule, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fix != nil {
		t.Errorf("expected nil fix when no violation is resolvable, got %+v", fix)
	}
}
