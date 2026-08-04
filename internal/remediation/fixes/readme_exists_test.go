package fixes

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
)

func TestReadmeExistsFixer_AddsStub(t *testing.T) {
	mux := http.NewServeMux() // no endpoints registered — fixer must not need to fetch anything
	client := newTestClient(t, mux)

	repo := gh.Repository{Owner: "o", Name: "scanned-repo", FullName: "o/scanned-repo", DefaultBranch: "main"}
	rule := config.Rule{ID: "readme-exists"}
	result := compliance.Result{RuleID: "readme-exists", Repo: "o/scanned-repo", Status: compliance.StatusFail}

	f := &readmeExistsFixer{}
	fix, err := f.Remediate(context.Background(), client, repo, rule, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fix == nil || len(fix.Files) != 1 {
		t.Fatalf("expected exactly one file change, got %+v", fix)
	}
	if fix.Files[0].Path != "README.md" {
		t.Errorf("got path=%q, want README.md", fix.Files[0].Path)
	}
	if !strings.Contains(string(fix.Files[0].Content), "# scanned-repo") {
		t.Errorf("expected README to reference the repo name, got:\n%s", fix.Files[0].Content)
	}
	if fix.PRTitle == "" || fix.CommitMessage == "" {
		t.Error("expected non-empty PR title and commit message")
	}
}
