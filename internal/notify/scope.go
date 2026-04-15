package notify

import (
	"fmt"
	"strings"

	"github.com/eukarya-inc/git-cascade/internal/config"
)

// scopeSummary returns a human-readable one-line summary of the scan scope.
// Boolean filters are shown as key=true/false pairs; include/exclude repo lists
// are expanded as comma-separated values.
func scopeSummary(scope config.Scope) string {
	var parts []string

	parts = append(parts, fmt.Sprintf("include_public=%v", config.BoolDefault(scope.IncludePublic, true)))
	parts = append(parts, fmt.Sprintf("include_private=%v", config.BoolDefault(scope.IncludePrivate, true)))
	parts = append(parts, fmt.Sprintf("include_archived=%v", config.BoolDefault(scope.IncludeArchived, false)))
	parts = append(parts, fmt.Sprintf("include_forked=%v", config.BoolDefault(scope.IncludeForked, false)))

	if len(scope.IncludeRepos) > 0 {
		parts = append(parts, fmt.Sprintf("include_repos=%s", strings.Join(scope.IncludeRepos, ", ")))
	}
	if len(scope.ExcludeRepos) > 0 {
		parts = append(parts, fmt.Sprintf("exclude_repos=%s", strings.Join(scope.ExcludeRepos, ", ")))
	}

	return strings.Join(parts, " · ")
}
