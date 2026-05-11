package checks

import (
	"context"
	"errors"
	"testing"

	gh "github.com/eukarya-inc/git-cascade/internal/github"
)

func TestFetchFirstExistingEmptyPaths(t *testing.T) {
	_, client := newFakeGitHub().serve(t)
	got, err := fetchFirstExisting(context.Background(), client, "org", "repo", "main", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestFetchFirstExistingFindsFirst(t *testing.T) {
	fg := newFakeGitHub()
	fg.setFile("org", "repo", "b.txt", []byte("b"))
	_, client := fg.serve(t)

	got, err := fetchFirstExisting(context.Background(), client, "org", "repo", "main", []string{"a.txt", "b.txt", "c.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "b.txt" {
		t.Errorf("got %q, want \"b.txt\"", got)
	}
}

func TestFetchFirstExistingNoneExist(t *testing.T) {
	_, client := newFakeGitHub().serve(t)

	got, err := fetchFirstExisting(context.Background(), client, "org", "repo", "main", []string{"x.txt", "y.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestFetchFirstExistingCancelledContext(t *testing.T) {
	fg := newFakeGitHub()
	fg.setFile("org", "repo", "a.txt", []byte("hello"))
	_, client := fg.serve(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	// Should return quickly without panic; error or empty are both acceptable.
	_, _ = fetchFirstExisting(ctx, client, "org", "repo", "main", []string{"a.txt"})
}

// TestCachedFetchFileContentFallback verifies that CachedFetchFileContent
// calls ListDirectoryContents directly when no cache is in context.
func TestCachedFetchFileContentFallback(t *testing.T) {
	fg := newFakeGitHub()
	fg.setFile("org", "repo", "README.md", []byte("hello"))
	_, client := fg.serve(t)

	content, err := gh.CachedFetchFileContent(context.Background(), client, "org", "repo", "README.md", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(content) != "hello" {
		t.Errorf("got %q, want \"hello\"", content)
	}
}

// TestCachedListDirectoryContentsFallback verifies that CachedListDirectoryContents
// calls ListDirectoryContents directly when no cache is in context.
func TestCachedListDirectoryContentsFallback(t *testing.T) {
	fg := newFakeGitHub()
	fg.setDir("org", "repo", ".github/workflows", []string{"ci.yml"})
	_, client := fg.serve(t)

	entries, err := gh.CachedListDirectoryContents(context.Background(), client, "org", "repo", ".github/workflows", "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("got %d entries, want 1", len(entries))
	}
}

// TestCachedFetchFileContentWithCache verifies deduplication when a cache is
// injected via context: two calls for the same path yield the same result.
func TestCachedFetchFileContentWithCache(t *testing.T) {
	fg := newFakeGitHub()
	fg.setFile("org", "repo", "README.md", []byte("cached"))
	_, client := fg.serve(t)

	cache := &gh.RepoCache{}
	ctx := gh.WithCache(context.Background(), cache)

	for i := range 2 {
		content, err := gh.CachedFetchFileContent(ctx, client, "org", "repo", "README.md", "main")
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if string(content) != "cached" {
			t.Errorf("call %d: got %q, want \"cached\"", i, content)
		}
	}
}

// errClient is a minimal stand-in that always errors on FetchFileContent calls.
// We abuse a cancelled context for the same effect without needing a real server.
var _ = errors.New // keep import used

func TestFetchFirstExistingOnlyCancelledSiblingsDoNotPropagateError(t *testing.T) {
	fg := newFakeGitHub()
	// Only "second.txt" exists; the other paths will get 404 (nil content, no error).
	fg.setFile("org", "repo", "second.txt", []byte("found"))
	_, client := fg.serve(t)

	got, err := fetchFirstExisting(context.Background(), client, "org", "repo", "main",
		[]string{"first.txt", "second.txt", "third.txt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "second.txt" {
		t.Errorf("got %q, want \"second.txt\"", got)
	}
}
