package github

import (
	"net/http"
	"testing"
)

func TestGetBranchRules_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/o/r/rules/branches/main", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []map[string]any{
			{
				"type": "pull_request",
				"parameters": map[string]any{
					"required_approving_review_count": 2,
				},
			},
		})
	})
	client := newTestClient(t, mux)

	rules, statusCode, err := GetBranchRules(t.Context(), client, "o", "r", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if statusCode != 0 {
		t.Errorf("expected statusCode 0 on success, got %d", statusCode)
	}
	if len(rules.PullRequest) != 1 {
		t.Fatalf("expected 1 pull request rule, got %d", len(rules.PullRequest))
	}
}

func TestGetBranchRules_NonRetryableError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/o/r/rules/branches/main", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	client := newTestClient(t, mux)

	rules, statusCode, err := GetBranchRules(t.Context(), client, "o", "r", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rules != nil {
		t.Errorf("expected nil rules, got %+v", rules)
	}
	if statusCode != http.StatusNotFound {
		t.Errorf("got statusCode %d, want %d", statusCode, http.StatusNotFound)
	}
}
