package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

// — EnsureBranch ——————————————————————————————————————————————————————————————

func TestEnsureBranch_CreatesWhenMissing(t *testing.T) {
	var created bool
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/o/r/git/ref/heads/newbranch", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/refs", func(w http.ResponseWriter, r *http.Request) {
		var body github.CreateRef
		json.NewDecoder(r.Body).Decode(&body)
		if body.Ref != "refs/heads/newbranch" || body.SHA != "abc123" {
			t.Errorf("unexpected CreateRef body: %+v", body)
		}
		created = true
		writeJSON(w, &github.Reference{Ref: github.Ptr(body.Ref), Object: &github.GitObject{SHA: github.Ptr(body.SHA)}})
	})
	client := newTestClient(t, mux)

	if err := EnsureBranch(context.Background(), client, "o", "r", "newbranch", "abc123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created {
		t.Error("expected CreateRef to be called")
	}
}

func TestEnsureBranch_ErrorsOnNon404GetRefFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/o/r/git/ref/heads/newbranch", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
	})
	client := newTestClient(t, mux)

	err := EnsureBranch(context.Background(), client, "o", "r", "newbranch", "abc123")
	if err == nil {
		t.Fatal("expected error on a non-404 GetRef failure")
	}
	if !strings.Contains(err.Error(), "checking branch") {
		t.Errorf("expected wrapped 'checking branch' error, got: %v", err)
	}
}

func TestEnsureBranch_ErrorsOnCreateRefFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/o/r/git/ref/heads/newbranch", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/refs", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
	})
	client := newTestClient(t, mux)

	err := EnsureBranch(context.Background(), client, "o", "r", "newbranch", "abc123")
	if err == nil {
		t.Fatal("expected error on CreateRef failure")
	}
	if !strings.Contains(err.Error(), "creating branch") {
		t.Errorf("expected wrapped 'creating branch' error, got: %v", err)
	}
}

func TestEnsureBranch_NoOpWhenExists(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/o/r/git/ref/heads/existing", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Reference{Ref: github.Ptr("refs/heads/existing"), Object: &github.GitObject{SHA: github.Ptr("abc123")}})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/refs", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("CreateRef should not be called when branch already exists")
	})
	client := newTestClient(t, mux)

	if err := EnsureBranch(context.Background(), client, "o", "r", "existing", "abc123"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// — CommitFiles ———————————————————————————————————————————————————————————————

func TestCommitFiles_CreatesCommitAndUpdatesRef(t *testing.T) {
	var sawTree, sawCommit, sawUpdateRef bool
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/o/r/git/ref/heads/fix", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Reference{Ref: github.Ptr("refs/heads/fix"), Object: &github.GitObject{SHA: github.Ptr("head1")}})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/commits/head1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Commit{SHA: github.Ptr("head1"), Tree: &github.Tree{SHA: github.Ptr("tree1")}})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/trees", func(w http.ResponseWriter, r *http.Request) {
		sawTree = true
		writeJSON(w, &github.Tree{SHA: github.Ptr("tree2")})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/commits", func(w http.ResponseWriter, r *http.Request) {
		sawCommit = true
		var body github.Commit
		json.NewDecoder(r.Body).Decode(&body)
		if body.GetMessage() != "fix it" {
			t.Errorf("unexpected commit message: %q", body.GetMessage())
		}
		writeJSON(w, &github.Commit{SHA: github.Ptr("commit2")})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/refs/heads/fix", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			http.NotFound(w, r)
			return
		}
		sawUpdateRef = true
		var body github.UpdateRef
		json.NewDecoder(r.Body).Decode(&body)
		if body.SHA != "commit2" {
			t.Errorf("unexpected UpdateRef sha: %q", body.SHA)
		}
		writeJSON(w, &github.Reference{Ref: github.Ptr("refs/heads/fix"), Object: &github.GitObject{SHA: github.Ptr("commit2")}})
	})
	client := newTestClient(t, mux)

	sha, err := CommitFiles(context.Background(), client, "o", "r", "fix", "fix it", nil, []FileWrite{
		{Path: "a.txt", Content: []byte("hello")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sha != "commit2" {
		t.Errorf("got sha=%q, want commit2", sha)
	}
	if !sawTree || !sawCommit || !sawUpdateRef {
		t.Errorf("missing calls: tree=%v commit=%v updateRef=%v", sawTree, sawCommit, sawUpdateRef)
	}
}

func TestCommitFiles_NoopWhenTreeUnchanged(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/o/r/git/ref/heads/fix", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Reference{Ref: github.Ptr("refs/heads/fix"), Object: &github.GitObject{SHA: github.Ptr("head1")}})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/commits/head1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Commit{SHA: github.Ptr("head1"), Tree: &github.Tree{SHA: github.Ptr("tree1")}})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/trees", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Tree{SHA: github.Ptr("tree1")}) // identical to current tree
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/commits", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("CreateCommit should not be called when the tree is unchanged")
	})
	client := newTestClient(t, mux)

	sha, err := CommitFiles(context.Background(), client, "o", "r", "fix", "no-op", nil, []FileWrite{
		{Path: "a.txt", Content: []byte("hello")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sha != "" {
		t.Errorf("got sha=%q, want empty (no-op)", sha)
	}
}

func TestCommitFiles_ErrorsWhenBranchRefFetchFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/o/r/git/ref/heads/fix", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	client := newTestClient(t, mux)

	_, err := CommitFiles(context.Background(), client, "o", "r", "fix", "msg", nil, []FileWrite{{Path: "a.txt", Content: []byte("x")}})
	if err == nil {
		t.Fatal("expected error when the branch ref can't be fetched")
	}
	if !strings.Contains(err.Error(), "fetching branch") {
		t.Errorf("expected wrapped 'fetching branch' error, got: %v", err)
	}
}

func TestCommitFiles_ErrorsWhenHeadCommitFetchFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/o/r/git/ref/heads/fix", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Reference{Ref: github.Ptr("refs/heads/fix"), Object: &github.GitObject{SHA: github.Ptr("head1")}})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/commits/head1", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
	})
	client := newTestClient(t, mux)

	_, err := CommitFiles(context.Background(), client, "o", "r", "fix", "msg", nil, []FileWrite{{Path: "a.txt", Content: []byte("x")}})
	if err == nil {
		t.Fatal("expected error when the head commit can't be fetched")
	}
	if !strings.Contains(err.Error(), "fetching head commit") {
		t.Errorf("expected wrapped 'fetching head commit' error, got: %v", err)
	}
}

func TestCommitFiles_ErrorsWhenCreateTreeFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/o/r/git/ref/heads/fix", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Reference{Ref: github.Ptr("refs/heads/fix"), Object: &github.GitObject{SHA: github.Ptr("head1")}})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/commits/head1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Commit{SHA: github.Ptr("head1"), Tree: &github.Tree{SHA: github.Ptr("tree1")}})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/trees", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
	})
	client := newTestClient(t, mux)

	_, err := CommitFiles(context.Background(), client, "o", "r", "fix", "msg", nil, []FileWrite{{Path: "a.txt", Content: []byte("x")}})
	if err == nil {
		t.Fatal("expected error when CreateTree fails")
	}
	if !strings.Contains(err.Error(), "creating tree") {
		t.Errorf("expected wrapped 'creating tree' error, got: %v", err)
	}
}

func TestCommitFiles_ErrorsWhenCreateCommitFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/o/r/git/ref/heads/fix", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Reference{Ref: github.Ptr("refs/heads/fix"), Object: &github.GitObject{SHA: github.Ptr("head1")}})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/commits/head1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Commit{SHA: github.Ptr("head1"), Tree: &github.Tree{SHA: github.Ptr("tree1")}})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/trees", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Tree{SHA: github.Ptr("tree2")}) // changed, so CreateCommit is attempted
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/commits", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
	})
	client := newTestClient(t, mux)

	_, err := CommitFiles(context.Background(), client, "o", "r", "fix", "msg", nil, []FileWrite{{Path: "a.txt", Content: []byte("x")}})
	if err == nil {
		t.Fatal("expected error when CreateCommit fails")
	}
	if !strings.Contains(err.Error(), "creating commit") {
		t.Errorf("expected wrapped 'creating commit' error, got: %v", err)
	}
}

func TestCommitFiles_ErrorsWhenUpdateRefFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/o/r/git/ref/heads/fix", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Reference{Ref: github.Ptr("refs/heads/fix"), Object: &github.GitObject{SHA: github.Ptr("head1")}})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/commits/head1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Commit{SHA: github.Ptr("head1"), Tree: &github.Tree{SHA: github.Ptr("tree1")}})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/trees", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Tree{SHA: github.Ptr("tree2")})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/commits", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Commit{SHA: github.Ptr("commit2")})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/refs/heads/fix", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
	})
	client := newTestClient(t, mux)

	_, err := CommitFiles(context.Background(), client, "o", "r", "fix", "msg", nil, []FileWrite{{Path: "a.txt", Content: []byte("x")}})
	if err == nil {
		t.Fatal("expected error when UpdateRef fails")
	}
	if !strings.Contains(err.Error(), "updating branch") {
		t.Errorf("expected wrapped 'updating branch' error, got: %v", err)
	}
}

func TestCommitFiles_UsesProvidedAuthor(t *testing.T) {
	var gotAuthor, gotCommitter string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/o/r/git/ref/heads/fix", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Reference{Ref: github.Ptr("refs/heads/fix"), Object: &github.GitObject{SHA: github.Ptr("head1")}})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/commits/head1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Commit{SHA: github.Ptr("head1"), Tree: &github.Tree{SHA: github.Ptr("tree1")}})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/trees", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Tree{SHA: github.Ptr("tree2")})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/commits", func(w http.ResponseWriter, r *http.Request) {
		var body github.Commit
		json.NewDecoder(r.Body).Decode(&body)
		gotAuthor = body.GetAuthor().GetName()
		gotCommitter = body.GetCommitter().GetName()
		writeJSON(w, &github.Commit{SHA: github.Ptr("commit2")})
	})
	mux.HandleFunc("/api/v3/repos/o/r/git/refs/heads/fix", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, &github.Reference{Ref: github.Ptr("refs/heads/fix"), Object: &github.GitObject{SHA: github.Ptr("commit2")}})
	})
	client := newTestClient(t, mux)

	name := "git-cascade"
	_, err := CommitFiles(context.Background(), client, "o", "r", "fix", "msg", &github.CommitAuthor{Name: &name}, []FileWrite{
		{Path: "a.txt", Content: []byte("x")},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuthor != "git-cascade" || gotCommitter != "git-cascade" {
		t.Errorf("expected author/committer=git-cascade, got author=%q committer=%q", gotAuthor, gotCommitter)
	}
}

// — CreateOrUpdatePullRequest ————————————————————————————————————————————————

func TestCreateOrUpdatePullRequest_ReturnsExistingWhenOpen(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/o/r/pulls", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, []*github.PullRequest{{HTMLURL: github.Ptr("https://github.com/o/r/pull/1")}})
		case http.MethodPost:
			t.Fatal("Create should not be called when an open PR already exists")
		}
	})
	client := newTestClient(t, mux)

	url, err := CreateOrUpdatePullRequest(context.Background(), client, "o", "r", "fix", "main", "title", "body", nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://github.com/o/r/pull/1" {
		t.Errorf("got url=%q", url)
	}
}

func TestCreateOrUpdatePullRequest_CreatesAndLabels(t *testing.T) {
	var labeled []string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/o/r/pulls", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, []*github.PullRequest{})
		case http.MethodPost:
			writeJSON(w, &github.PullRequest{Number: github.Ptr(7), HTMLURL: github.Ptr("https://github.com/o/r/pull/7")})
		}
	})
	mux.HandleFunc("/api/v3/repos/o/r/issues/7/labels", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&labeled)
		writeJSON(w, []*github.Label{})
	})
	client := newTestClient(t, mux)

	url, err := CreateOrUpdatePullRequest(context.Background(), client, "o", "r", "fix", "main", "title", "body", []string{"automated-fix"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://github.com/o/r/pull/7" {
		t.Errorf("got url=%q", url)
	}
	if len(labeled) != 1 || labeled[0] != "automated-fix" {
		t.Errorf("got labeled=%v", labeled)
	}
}

func TestCreateOrUpdatePullRequest_ErrorsWhenListFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/o/r/pulls", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
	})
	client := newTestClient(t, mux)

	_, err := CreateOrUpdatePullRequest(context.Background(), client, "o", "r", "fix", "main", "title", "body", nil, false)
	if err == nil {
		t.Fatal("expected error when listing pull requests fails")
	}
	if !strings.Contains(err.Error(), "listing pull requests") {
		t.Errorf("expected wrapped 'listing pull requests' error, got: %v", err)
	}
}

func TestCreateOrUpdatePullRequest_ErrorsWhenCreateFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/o/r/pulls", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, []*github.PullRequest{})
		case http.MethodPost:
			http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
		}
	})
	client := newTestClient(t, mux)

	_, err := CreateOrUpdatePullRequest(context.Background(), client, "o", "r", "fix", "main", "title", "body", nil, false)
	if err == nil {
		t.Fatal("expected error when creating the pull request fails")
	}
	if !strings.Contains(err.Error(), "creating pull request") {
		t.Errorf("expected wrapped 'creating pull request' error, got: %v", err)
	}
}

func TestCreateOrUpdatePullRequest_ErrorsWhenLabelingFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/o/r/pulls", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, []*github.PullRequest{})
		case http.MethodPost:
			writeJSON(w, &github.PullRequest{Number: github.Ptr(7), HTMLURL: github.Ptr("https://github.com/o/r/pull/7")})
		}
	})
	mux.HandleFunc("/api/v3/repos/o/r/issues/7/labels", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
	})
	client := newTestClient(t, mux)

	_, err := CreateOrUpdatePullRequest(context.Background(), client, "o", "r", "fix", "main", "title", "body", []string{"automated-fix"}, false)
	if err == nil {
		t.Fatal("expected error when labeling the pull request fails")
	}
	if !strings.Contains(err.Error(), "labeling pull request") {
		t.Errorf("expected wrapped 'labeling pull request' error, got: %v", err)
	}
}
