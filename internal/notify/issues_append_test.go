package notify

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	"github.com/google/go-github/v84/github"
)

// fakeIssuesAPI is a minimal in-memory GitHub Issues API used to exercise the
// mode=append code path (findOrCreateIssueByTitle, findSectionComment,
// postAppendIssue) without hitting the real GitHub API.
type fakeIssuesAPI struct {
	nextIssueID   int64
	nextCommentID int64
	issues        []*github.Issue
	comments      map[int64][]*github.IssueComment // issue number -> comments
}

func newFakeIssuesAPI() *fakeIssuesAPI {
	return &fakeIssuesAPI{
		nextIssueID:   1,
		nextCommentID: 1,
		comments:      make(map[int64][]*github.IssueComment),
	}
}

func (f *fakeIssuesAPI) serve(t *testing.T) *github.Client {
	t.Helper()
	mux := http.NewServeMux()

	// GET/POST /repos/{owner}/{repo}/issues
	mux.HandleFunc("/api/v3/repos/o/r/issues", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(f.issues)
		case http.MethodPost:
			var req github.IssueRequest
			json.NewDecoder(r.Body).Decode(&req)
			num := int(f.nextIssueID)
			f.nextIssueID++
			issue := &github.Issue{
				Number:  &num,
				Title:   req.Title,
				Body:    req.Body,
				HTMLURL: github.Ptr("https://github.com/o/r/issues/" + strconv.Itoa(num)),
			}
			f.issues = append(f.issues, issue)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(issue)
		default:
			http.NotFound(w, r)
		}
	})

	// GET/POST /repos/{owner}/{repo}/issues/{number}/comments
	mux.HandleFunc("/api/v3/repos/o/r/issues/", func(w http.ResponseWriter, r *http.Request) {
		rest := r.URL.Path[len("/api/v3/repos/o/r/issues/"):]
		parts := splitPath(rest)
		if len(parts) < 2 || parts[1] != "comments" {
			http.NotFound(w, r)
			return
		}
		num, _ := strconv.Atoi(parts[0])
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(f.comments[int64(num)])
		case http.MethodPost:
			var c github.IssueComment
			json.NewDecoder(r.Body).Decode(&c)
			id := f.nextCommentID
			f.nextCommentID++
			c.ID = &id
			f.comments[int64(num)] = append(f.comments[int64(num)], &c)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(&c)
		default:
			http.NotFound(w, r)
		}
	})

	// PATCH /repos/{owner}/{repo}/issues/comments/{id}
	mux.HandleFunc("/api/v3/repos/o/r/issues/comments/", func(w http.ResponseWriter, r *http.Request) {
		idStr := r.URL.Path[len("/api/v3/repos/o/r/issues/comments/"):]
		id, _ := strconv.ParseInt(idStr, 10, 64)
		var req github.IssueComment
		json.NewDecoder(r.Body).Decode(&req)
		for _, list := range f.comments {
			for _, c := range list {
				if c.GetID() == id {
					c.Body = req.Body
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(c)
					return
				}
			}
		}
		http.NotFound(w, r)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := github.NewClient(nil)
	baseURL := srv.URL + "/api/v3/"
	client, _ = client.WithEnterpriseURLs(baseURL, baseURL)
	return client
}

func splitPath(s string) []string {
	var out []string
	cur := ""
	for _, ch := range s {
		if ch == '/' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(ch)
	}
	out = append(out, cur)
	return out
}

func (f *fakeIssuesAPI) commentBodies(issueNumber int) []string {
	var out []string
	for _, c := range f.comments[int64(issueNumber)] {
		out = append(out, c.GetBody())
	}
	return out
}

// — findOrCreateIssueByTitle ——————————————————————————————————————————————————

func TestFindOrCreateIssueByTitle_CreatesWhenMissing(t *testing.T) {
	fake := newFakeIssuesAPI()
	client := fake.serve(t)

	num, url, err := findOrCreateIssueByTitle(t.Context(), client, "o", "r", "Integrated Findings", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if num != 1 {
		t.Errorf("expected issue #1, got #%d", num)
	}
	if url == "" {
		t.Error("expected non-empty HTML URL")
	}
	if len(fake.issues) != 1 || fake.issues[0].GetTitle() != "Integrated Findings" {
		t.Errorf("expected one created issue titled 'Integrated Findings', got %+v", fake.issues)
	}
}

func TestFindOrCreateIssueByTitle_FindsExisting(t *testing.T) {
	fake := newFakeIssuesAPI()
	client := fake.serve(t)

	// Pre-seed an issue as if another tool created it.
	existingNum := 42
	fake.issues = append(fake.issues, &github.Issue{
		Number:  &existingNum,
		Title:   github.Ptr("Integrated Findings"),
		HTMLURL: github.Ptr("https://github.com/o/r/issues/42"),
	})

	num, url, err := findOrCreateIssueByTitle(t.Context(), client, "o", "r", "Integrated Findings", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if num != 42 {
		t.Errorf("expected to find existing issue #42, got #%d", num)
	}
	if url != "https://github.com/o/r/issues/42" {
		t.Errorf("got %q", url)
	}
	if len(fake.issues) != 1 {
		t.Error("expected no new issue to be created")
	}
}

// — findSectionComment ————————————————————————————————————————————————————————

func TestFindSectionComment_NoneYet(t *testing.T) {
	fake := newFakeIssuesAPI()
	client := fake.serve(t)

	id, err := findSectionComment(t.Context(), client, "o", "r", 1, "myorg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 0 {
		t.Errorf("expected 0, got %d", id)
	}
}

func TestFindSectionComment_MatchesOwnKeyOnly(t *testing.T) {
	fake := newFakeIssuesAPI()
	client := fake.serve(t)

	fake.comments[1] = []*github.IssueComment{
		{ID: github.Ptr(int64(10)), Body: github.Ptr(sectionCommentPrefix + "team-a -->\nfindings")},
		{ID: github.Ptr(int64(11)), Body: github.Ptr(sectionCommentPrefix + "team-b -->\nfindings")},
	}

	id, err := findSectionComment(t.Context(), client, "o", "r", 1, "team-b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 11 {
		t.Errorf("expected comment 11 (team-b), got %d", id)
	}
}

// — postAppendIssue ———————————————————————————————————————————————————————————

func TestPostAppendIssue_CreatesIssueAndComment(t *testing.T) {
	fake := newFakeIssuesAPI()
	client := fake.serve(t)

	cfg := config.IssuesConfig{
		Mode:           "append",
		ComplianceRepo: "o/r",
		IssueTitle:     "Integrated Findings",
	}
	results := []compliance.Result{makeResult("o/myrepo", compliance.StatusFail, config.SeverityError, false)}

	url, err := postAppendIssue(t.Context(), client, cfg, "myorg", results, "", config.Scope{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url == "" {
		t.Error("expected non-empty issue URL")
	}
	if len(fake.issues) != 1 {
		t.Fatalf("expected one issue created, got %d", len(fake.issues))
	}
	bodies := fake.commentBodies(fake.issues[0].GetNumber())
	if len(bodies) != 1 {
		t.Fatalf("expected one comment, got %d", len(bodies))
	}
	if got := bodies[0]; !hasPrefix(got, sectionCommentPrefix+"myorg -->") {
		t.Errorf("comment missing section marker for default org key: %q", got)
	}
}

func TestPostAppendIssue_IdempotentAcrossRuns(t *testing.T) {
	fake := newFakeIssuesAPI()
	client := fake.serve(t)

	cfg := config.IssuesConfig{
		Mode:           "append",
		ComplianceRepo: "o/r",
		IssueTitle:     "Integrated Findings",
	}
	results := []compliance.Result{makeResult("o/myrepo", compliance.StatusFail, config.SeverityError, false)}

	if _, err := postAppendIssue(t.Context(), client, cfg, "myorg", results, "", config.Scope{}); err != nil {
		t.Fatalf("first run: unexpected error: %v", err)
	}
	if _, err := postAppendIssue(t.Context(), client, cfg, "myorg", results, "", config.Scope{}); err != nil {
		t.Fatalf("second run: unexpected error: %v", err)
	}

	if len(fake.issues) != 1 {
		t.Fatalf("expected the shared issue to be reused, not recreated: got %d issues", len(fake.issues))
	}
	bodies := fake.commentBodies(fake.issues[0].GetNumber())
	if len(bodies) != 1 {
		t.Fatalf("expected the existing comment to be edited in place, not duplicated: got %d comments", len(bodies))
	}
}

func TestPostAppendIssue_DistinctSectionKeysGetSeparateComments(t *testing.T) {
	fake := newFakeIssuesAPI()
	client := fake.serve(t)

	results := []compliance.Result{makeResult("o/myrepo", compliance.StatusFail, config.SeverityError, false)}

	cfgA := config.IssuesConfig{Mode: "append", ComplianceRepo: "o/r", IssueTitle: "Integrated Findings", SectionKey: "team-a"}
	cfgB := config.IssuesConfig{Mode: "append", ComplianceRepo: "o/r", IssueTitle: "Integrated Findings", SectionKey: "team-b"}

	if _, err := postAppendIssue(t.Context(), client, cfgA, "myorg", results, "", config.Scope{}); err != nil {
		t.Fatalf("team-a run: unexpected error: %v", err)
	}
	if _, err := postAppendIssue(t.Context(), client, cfgB, "myorg", results, "", config.Scope{}); err != nil {
		t.Fatalf("team-b run: unexpected error: %v", err)
	}

	if len(fake.issues) != 1 {
		t.Fatalf("expected both configs to share one issue, got %d", len(fake.issues))
	}
	bodies := fake.commentBodies(fake.issues[0].GetNumber())
	if len(bodies) != 2 {
		t.Fatalf("expected two distinct section comments, got %d", len(bodies))
	}
}

func TestPostAppendIssue_InvalidRepo(t *testing.T) {
	cfg := config.IssuesConfig{Mode: "append", ComplianceRepo: "not-a-valid-repo", IssueTitle: "x"}
	_, err := postAppendIssue(t.Context(), nil, cfg, "myorg", nil, "", config.Scope{})
	if err == nil {
		t.Fatal("expected error for invalid repo format")
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
