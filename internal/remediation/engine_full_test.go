package remediation

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/google/go-github/v90/github"
)

func newTestClient(t *testing.T, mux *http.ServeMux) *github.Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client, _ := github.NewClient(github.WithAuthToken("fake-token"), github.WithEnterpriseURLs(srv.URL+"/", srv.URL+"/"))
	return client
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// TestRun_OpensPRForRealFix exercises the full remediateOne path: resolve the
// default branch HEAD, create the fix branch, commit the files, and open a
// PR — verifying the branch name, commit author, and PR labels all come from
// RemediationConfig as expected.
func TestRun_OpensPRForRealFix(t *testing.T) {
	const branch = "git-cascade/fix/test-rule"
	var branchExists bool
	var gotAuthor string
	var gotLabels []string

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/o/r/git/ref/heads/main", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Reference{Ref: github.Ptr("refs/heads/main"), Object: &github.GitObject{SHA: github.Ptr("base1")}})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/ref/heads/"+branch, func(w http.ResponseWriter, r *http.Request) {
		if !branchExists {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, &github.Reference{Ref: github.Ptr("refs/heads/" + branch), Object: &github.GitObject{SHA: github.Ptr("base1")}})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/refs", func(w http.ResponseWriter, r *http.Request) {
		var body github.CreateRef
		json.NewDecoder(r.Body).Decode(&body)
		if body.Ref != "refs/heads/"+branch || body.SHA != "base1" {
			t.Errorf("unexpected CreateRef body: %+v", body)
		}
		branchExists = true
		writeJSON(w, &github.Reference{Ref: github.Ptr(body.Ref), Object: &github.GitObject{SHA: github.Ptr(body.SHA)}})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/commits/base1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Commit{SHA: github.Ptr("base1"), Tree: &github.Tree{SHA: github.Ptr("tree1")}})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/trees", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Tree{SHA: github.Ptr("tree2")})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/commits", func(w http.ResponseWriter, r *http.Request) {
		var body github.Commit
		json.NewDecoder(r.Body).Decode(&body)
		gotAuthor = body.GetAuthor().GetName()
		if body.GetMessage() != "fix it" {
			t.Errorf("unexpected commit message: %q", body.GetMessage())
		}
		writeJSON(w, &github.Commit{SHA: github.Ptr("commit2")})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/refs/heads/"+branch, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Reference{Ref: github.Ptr("refs/heads/" + branch), Object: &github.GitObject{SHA: github.Ptr("commit2")}})
	})
	mux.HandleFunc("/api/v3/repos/o/r/pulls", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, []*github.PullRequest{})
		case http.MethodPost:
			writeJSON(w, &github.PullRequest{Number: github.Ptr(5), HTMLURL: github.Ptr("https://github.com/o/r/pull/5")})
		}
	})
	mux.HandleFunc("/api/v3/repos/o/r/issues/5/labels", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotLabels)
		writeJSON(w, []*github.Label{})
	})
	client := newTestClient(t, mux)

	r := &fakeRemediator{id: "test-rule", fix: &Fix{
		Files:         []FileChange{{Path: "a.txt", Content: []byte("hi")}},
		CommitMessage: "fix it",
		PRTitle:       "git-cascade: fix it",
		PRBody:        "body",
	}}
	Register(r)
	defer delete(registry, "test-rule")

	rules := map[string]config.Rule{"test-rule": {ID: "test-rule", AutoRemediation: boolPtr(true)}}
	results := []compliance.Result{{RuleID: "test-rule", Repo: "o/r", Status: compliance.StatusFail}}
	repos := map[string]gh.Repository{"o/r": {Owner: "o", Name: "r", FullName: "o/r", DefaultBranch: "main"}}
	cfg := config.RemediationConfig{
		Enabled: true,
		CommitAuthor: config.CommitAuthorConfig{
			Name:  "git-cascade",
			Email: "git-cascade@example.com",
		},
		PRLabels: []string{"automated-fix"},
	}

	outcomes := Run(context.Background(), client, cfg, results, rules, repos, slog.Default())
	if len(outcomes) != 1 {
		t.Fatalf("expected 1 outcome, got %d: %+v", len(outcomes), outcomes)
	}
	got := outcomes[0]
	if got.Err != nil {
		t.Fatalf("unexpected error: %v", got.Err)
	}
	if got.PRURL != "https://github.com/o/r/pull/5" {
		t.Errorf("got PRURL=%q", got.PRURL)
	}
	if gotAuthor != "git-cascade" {
		t.Errorf("expected commit author git-cascade, got %q", gotAuthor)
	}
	if len(gotLabels) != 1 || gotLabels[0] != "automated-fix" {
		t.Errorf("got labels=%v", gotLabels)
	}
}

// TestRun_UsesCustomBranchPrefix verifies RemediationConfig.BranchPrefix
// overrides the "git-cascade/fix" default in the created branch name.
func TestRun_UsesCustomBranchPrefix(t *testing.T) {
	const branch = "custom/fix/test-rule"
	var sawExpectedBranch bool

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/o/r/git/ref/heads/main", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Reference{Ref: github.Ptr("refs/heads/main"), Object: &github.GitObject{SHA: github.Ptr("base1")}})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/ref/heads/"+branch, func(w http.ResponseWriter, r *http.Request) {
		sawExpectedBranch = true
		http.NotFound(w, r)
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/refs", func(w http.ResponseWriter, r *http.Request) {
		// EnsureBranch creates the branch; remediateOne stops here for this
		// test since CommitFiles' subsequent GetCommit isn't mocked to succeed.
		writeJSON(w, &github.Reference{Ref: github.Ptr("refs/heads/" + branch), Object: &github.GitObject{SHA: github.Ptr("base1")}})
	})
	client := newTestClient(t, mux)

	r := &fakeRemediator{id: "test-rule", fix: &Fix{
		Files: []FileChange{{Path: "a.txt", Content: []byte("hi")}},
	}}
	Register(r)
	defer delete(registry, "test-rule")

	rules := map[string]config.Rule{"test-rule": {ID: "test-rule", AutoRemediation: boolPtr(true)}}
	results := []compliance.Result{{RuleID: "test-rule", Repo: "o/r", Status: compliance.StatusFail}}
	repos := map[string]gh.Repository{"o/r": {Owner: "o", Name: "r", FullName: "o/r", DefaultBranch: "main"}}
	cfg := config.RemediationConfig{Enabled: true, BranchPrefix: "custom/fix"}

	Run(context.Background(), client, cfg, results, rules, repos, slog.Default())
	if !sawExpectedBranch {
		t.Error("expected EnsureBranch to check the custom-prefixed branch name")
	}
}

// TestRun_ErrorWhenFixComputationFails verifies a Remediate error is wrapped
// and surfaced on the outcome rather than panicking or being swallowed.
func TestRun_ErrorWhenFixComputationFails(t *testing.T) {
	r := &fakeRemediator{id: "test-rule", err: errors.New("boom")}
	Register(r)
	defer delete(registry, "test-rule")

	rules := map[string]config.Rule{"test-rule": {ID: "test-rule", AutoRemediation: boolPtr(true)}}
	results := []compliance.Result{{RuleID: "test-rule", Repo: "o/r", Status: compliance.StatusFail}}
	repos := map[string]gh.Repository{"o/r": {Owner: "o", Name: "r", FullName: "o/r", DefaultBranch: "main"}}

	outcomes := Run(context.Background(), nil, config.RemediationConfig{Enabled: true}, results, rules, repos, slog.Default())
	if len(outcomes) != 1 || outcomes[0].Err == nil {
		t.Fatalf("expected 1 outcome with an error, got %+v", outcomes)
	}
	if !strings.Contains(outcomes[0].Err.Error(), "computing fix") {
		t.Errorf("expected wrapped 'computing fix' error, got: %v", outcomes[0].Err)
	}
}

// TestRun_ErrorWhenDefaultBranchNotFound verifies a missing default branch
// (GetBranchHEAD returns "") surfaces a clear error rather than proceeding.
func TestRun_ErrorWhenDefaultBranchNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/o/r/git/ref/heads/main", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	client := newTestClient(t, mux)

	r := &fakeRemediator{id: "test-rule", fix: &Fix{Files: []FileChange{{Path: "a.txt", Content: []byte("hi")}}}}
	Register(r)
	defer delete(registry, "test-rule")

	rules := map[string]config.Rule{"test-rule": {ID: "test-rule", AutoRemediation: boolPtr(true)}}
	results := []compliance.Result{{RuleID: "test-rule", Repo: "o/r", Status: compliance.StatusFail}}
	repos := map[string]gh.Repository{"o/r": {Owner: "o", Name: "r", FullName: "o/r", DefaultBranch: "main"}}

	outcomes := Run(context.Background(), client, config.RemediationConfig{Enabled: true}, results, rules, repos, slog.Default())
	if len(outcomes) != 1 || outcomes[0].Err == nil {
		t.Fatalf("expected 1 outcome with an error, got %+v", outcomes)
	}
	if !strings.Contains(outcomes[0].Err.Error(), "default branch") {
		t.Errorf("expected 'default branch ... not found' error, got: %v", outcomes[0].Err)
	}
}
