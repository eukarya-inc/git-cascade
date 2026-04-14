package github

import (
	"testing"

	"github.com/eukarya-inc/git-cascade/internal/config"
)

func testRepos() []Repository {
	return []Repository{
		{Name: "api", FullName: "org/api", Private: true},
		{Name: "web", FullName: "org/web", Private: false},
		{Name: "docs", FullName: "org/docs", Private: false},
		{Name: "infra", FullName: "org/infra", Private: true},
		{Name: "old-service", FullName: "org/old-service", Private: true, Archived: true},
	}
}

func repoNames(repos []Repository) []string {
	names := make([]string, len(repos))
	for i, r := range repos {
		names[i] = r.Name
	}
	return names
}

func boolPtr(b bool) *bool { return &b }

func TestFilterDefaults(t *testing.T) {
	f := RepoFilter{IncludePublic: true, IncludePrivate: true}
	got := repoNames(f.Apply(testRepos()))
	want := []string{"api", "web", "docs", "infra"}
	assertNames(t, got, want)
}

func TestFilterSkipPublic(t *testing.T) {
	f := RepoFilter{IncludePublic: false, IncludePrivate: true}
	got := repoNames(f.Apply(testRepos()))
	want := []string{"api", "infra"}
	assertNames(t, got, want)
}

func TestFilterSkipPrivate(t *testing.T) {
	f := RepoFilter{IncludePublic: true, IncludePrivate: false}
	got := repoNames(f.Apply(testRepos()))
	want := []string{"web", "docs"}
	assertNames(t, got, want)
}

func TestFilterIncludeArchived(t *testing.T) {
	f := RepoFilter{IncludePublic: true, IncludePrivate: true, IncludeArchived: true}
	got := repoNames(f.Apply(testRepos()))
	want := []string{"api", "web", "docs", "infra", "old-service"}
	assertNames(t, got, want)
}

func TestFilterExcludeRepos(t *testing.T) {
	f := RepoFilter{IncludePublic: true, IncludePrivate: true, ExcludeRepos: []string{"docs", "infra"}}
	got := repoNames(f.Apply(testRepos()))
	want := []string{"api", "web"}
	assertNames(t, got, want)
}

func TestFilterIncludeRepos(t *testing.T) {
	f := RepoFilter{
		IncludePublic:  false,
		IncludePrivate: false,
		IncludeRepos:   []string{"api", "docs"},
	}
	got := repoNames(f.Apply(testRepos()))
	want := []string{"api", "docs"}
	assertNames(t, got, want)
}

func TestFilterIncludeReposOverridesExclude(t *testing.T) {
	f := RepoFilter{
		IncludePublic:  true,
		IncludePrivate: true,
		IncludeRepos:   []string{"api", "web"},
		ExcludeRepos:   []string{"api"},
	}
	got := repoNames(f.Apply(testRepos()))
	want := []string{"api", "web"}
	assertNames(t, got, want)
}

func TestFilterSkipBothVisibilities(t *testing.T) {
	f := RepoFilter{IncludePublic: false, IncludePrivate: false}
	got := f.Apply(testRepos())
	if len(got) != 0 {
		t.Fatalf("expected 0 repos, got %d", len(got))
	}
}

func TestRepoFilterFromScope_Defaults(t *testing.T) {
	// Empty scope should default to public+private, no archived
	f := RepoFilterFromScope(config.Scope{})
	if !f.IncludePublic {
		t.Error("expected IncludePublic=true by default")
	}
	if !f.IncludePrivate {
		t.Error("expected IncludePrivate=true by default")
	}
	if f.IncludeArchived {
		t.Error("expected IncludeArchived=false by default")
	}
}

func TestRepoFilterFromScope_Explicit(t *testing.T) {
	f := RepoFilterFromScope(config.Scope{
		IncludePublic:   boolPtr(false),
		IncludePrivate:  boolPtr(true),
		IncludeArchived: boolPtr(true),
		ExcludeRepos:    []string{"sandbox"},
	})
	got := repoNames(f.Apply(testRepos()))
	want := []string{"api", "infra", "old-service"}
	assertNames(t, got, want)
}

func TestRepoFilterFromScope_IncludeRepos(t *testing.T) {
	f := RepoFilterFromScope(config.Scope{
		IncludeRepos: []string{"web", "infra"},
	})
	got := repoNames(f.Apply(testRepos()))
	want := []string{"web", "infra"}
	assertNames(t, got, want)
}

func assertNames(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("at index %d: expected %q, got %q (full: expected %v, got %v)", i, want[i], got[i], want, got)
		}
	}
}
