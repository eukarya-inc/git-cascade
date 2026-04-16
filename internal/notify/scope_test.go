package notify

import (
	"strings"
	"testing"

	"github.com/eukarya-inc/git-cascade/internal/config"
)

func boolPtr(b bool) *bool { return &b }

func TestScopeSummary_Defaults(t *testing.T) {
	// All nil — should use package defaults: public=true, private=true, archived=false, forked=false.
	got := scopeSummary(config.Scope{})

	assertContains(t, got, "include_public=true")
	assertContains(t, got, "include_private=true")
	assertContains(t, got, "include_archived=false")
	assertContains(t, got, "include_forked=false")

	// No include_repos / exclude_repos when lists are empty.
	if strings.Contains(got, "include_repos=") {
		t.Errorf("expected no include_repos part, got %q", got)
	}
	if strings.Contains(got, "exclude_repos=") {
		t.Errorf("expected no exclude_repos part, got %q", got)
	}
}

func TestScopeSummary_ExplicitBooleans(t *testing.T) {
	scope := config.Scope{
		IncludePublic:   boolPtr(false),
		IncludePrivate:  boolPtr(false),
		IncludeArchived: boolPtr(true),
		IncludeForked:   boolPtr(true),
	}
	got := scopeSummary(scope)

	assertContains(t, got, "include_public=false")
	assertContains(t, got, "include_private=false")
	assertContains(t, got, "include_archived=true")
	assertContains(t, got, "include_forked=true")
}

func TestScopeSummary_IncludeRepos(t *testing.T) {
	scope := config.Scope{
		IncludeRepos: []string{"org/api", "org/web"},
	}
	got := scopeSummary(scope)

	assertContains(t, got, "include_repos=org/api, org/web")
}

func TestScopeSummary_ExcludeRepos(t *testing.T) {
	scope := config.Scope{
		ExcludeRepos: []string{"org/sandbox"},
	}
	got := scopeSummary(scope)

	assertContains(t, got, "exclude_repos=org/sandbox")
}

func TestScopeSummary_BothLists(t *testing.T) {
	scope := config.Scope{
		IncludeRepos: []string{"org/api"},
		ExcludeRepos: []string{"org/sandbox", "org/test"},
	}
	got := scopeSummary(scope)

	assertContains(t, got, "include_repos=org/api")
	assertContains(t, got, "exclude_repos=org/sandbox, org/test")
}

func TestScopeSummary_Separator(t *testing.T) {
	got := scopeSummary(config.Scope{})
	// Parts must be joined with " · "
	if !strings.Contains(got, " · ") {
		t.Errorf("expected parts joined with \" · \", got %q", got)
	}
}

func assertContains(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Errorf("expected %q to contain %q", s, sub)
	}
}
