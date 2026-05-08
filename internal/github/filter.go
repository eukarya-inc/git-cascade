package github

import (
	"path"

	"github.com/eukarya-inc/git-cascade/internal/config"
)

// RepoFilter controls which repositories are included in a scan.
type RepoFilter struct {
	IncludePublic   bool
	IncludePrivate  bool
	IncludeArchived bool
	IncludeForked   bool
	// IncludeRepos, when non-empty, restricts the scan to only these repos (by name).
	// All other filters are ignored when this is set.
	IncludeRepos []string
	// ExcludeRepos removes these repos (by name) from the scan.
	ExcludeRepos []string
}

// RepoFilterFromScope creates a RepoFilter from a config Scope, applying
// sensible defaults (include public & private, exclude archived and forked).
func RepoFilterFromScope(scope config.Scope) RepoFilter {
	return RepoFilter{
		IncludePublic:   config.BoolDefault(scope.IncludePublic, true),
		IncludePrivate:  config.BoolDefault(scope.IncludePrivate, true),
		IncludeArchived: config.BoolDefault(scope.IncludeArchived, false),
		IncludeForked:   config.BoolDefault(scope.IncludeForked, false),
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
	var out []Repository
	for _, r := range repos {
		if matchesAny(r.Name, f.IncludeRepos) {
			out = append(out, r)
		}
	}
	return out
}

func (f RepoFilter) applyFilters(repos []Repository) []Repository {
	var out []Repository
	for _, r := range repos {
		if matchesAny(r.Name, f.ExcludeRepos) {
			continue
		}
		if !f.IncludeArchived && r.Archived {
			continue
		}
		if !f.IncludeForked && r.Fork {
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

// matchesAny reports whether name matches any of the given glob patterns.
// Exact strings are also supported (they are valid glob patterns).
// Malformed patterns never match.
func matchesAny(name string, patterns []string) bool {
	for _, p := range patterns {
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}
