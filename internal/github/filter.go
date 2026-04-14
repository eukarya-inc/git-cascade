package github

import (
	"github.com/eukarya-inc/git-cascade/internal/config"
)

// RepoFilter controls which repositories are included in a scan.
type RepoFilter struct {
	IncludePublic  bool
	IncludePrivate bool
	IncludeArchived bool
	// IncludeRepos, when non-empty, restricts the scan to only these repos (by name).
	// All other filters are ignored when this is set.
	IncludeRepos []string
	// ExcludeRepos removes these repos (by name) from the scan.
	ExcludeRepos []string
}

// RepoFilterFromScope creates a RepoFilter from a config Scope, applying
// sensible defaults (include public & private, exclude archived).
func RepoFilterFromScope(scope config.Scope) RepoFilter {
	return RepoFilter{
		IncludePublic:   config.BoolDefault(scope.IncludePublic, true),
		IncludePrivate:  config.BoolDefault(scope.IncludePrivate, true),
		IncludeArchived: config.BoolDefault(scope.IncludeArchived, false),
		IncludeRepos:    scope.IncludeRepos,
		ExcludeRepos:    scope.ExcludeRepos,
	}
}

// Apply filters a list of repositories according to the filter rules.
func (f RepoFilter) Apply(repos []Repository) []Repository {
	if len(f.IncludeRepos) > 0 {
		return f.applyIncludeList(repos)
	}
	return f.applyFilters(repos)
}

func (f RepoFilter) applyIncludeList(repos []Repository) []Repository {
	include := toSet(f.IncludeRepos)
	var out []Repository
	for _, r := range repos {
		if include[r.Name] {
			out = append(out, r)
		}
	}
	return out
}

func (f RepoFilter) applyFilters(repos []Repository) []Repository {
	exclude := toSet(f.ExcludeRepos)
	var out []Repository
	for _, r := range repos {
		if exclude[r.Name] {
			continue
		}
		if !f.IncludeArchived && r.Archived {
			continue
		}
		if !f.IncludePublic && !r.Private {
			continue
		}
		if !f.IncludePrivate && r.Private {
			continue
		}
		out = append(out, r)
	}
	return out
}

func toSet(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, item := range items {
		m[item] = true
	}
	return m
}
