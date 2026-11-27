package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/google/go-github/v90/github"
)

func newTreeTestClient(t *testing.T, handler http.HandlerFunc) *github.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client, err := github.NewClient(github.WithAuthToken("x"), github.WithEnterpriseURLs(srv.URL+"/", srv.URL+"/"))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestListTree_ReturnsBlobPaths(t *testing.T) {
	client := newTreeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/repos/o/r/git/ref/heads/release/v1" {
			json.NewEncoder(w).Encode(map[string]any{"ref": "refs/heads/release/v1", "object": map[string]any{"sha": "abc"}})
			return
		}
		if r.URL.Path != "/api/v3/repos/o/r/git/trees/abc" || r.URL.Query().Get("recursive") == "" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"sha": "abc",
			"tree": []map[string]any{
				{"path": "web", "type": "tree"},
				{"path": "web/Dockerfile", "type": "blob"},
				{"path": "sub", "type": "commit"},
			},
			"truncated": true,
		})
	})
	tree, err := ListTree(context.Background(), client, "o", "r", "release/v1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tree.Paths, []string{"web/Dockerfile"}) || !tree.Truncated {
		t.Errorf("got %+v", tree)
	}
}

func TestListTree_EmptyRepo(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusConflict} {
		client := newTreeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		})
		tree, err := ListTree(context.Background(), client, "o", "r", "main")
		if err != nil || tree != nil {
			t.Errorf("status %d: got (%v, %v), want (nil, nil)", status, tree, err)
		}
	}
}

func TestListTree_ErrorReturned(t *testing.T) {
	client := newTreeTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	if _, err := ListTree(context.Background(), client, "o", "r", "main"); err == nil {
		t.Error("expected error for 403")
	}
}
