package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/google/go-github/v84/github"
)

// aiConfigPaths are files/directories to inspect for executable hooks.
// Directory entries are scanned recursively one level deep.
var aiConfigPaths = []struct {
	path string
	dir  bool
}{
	{path: ".claude", dir: true},
	{path: ".cursor", dir: true},
	{path: ".mcp.json", dir: false},
}

type aiConfigSafetyChecker struct{}

func (c *aiConfigSafetyChecker) ID() string { return "ai-config-safety" }

func (c *aiConfigSafetyChecker) Check(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule) (*compliance.Result, error) {
	ref := repo.DefaultBranch

	var violations []string

	for _, entry := range aiConfigPaths {
		if entry.dir {
			v, err := checkAIConfigDir(ctx, client, repo, ref, entry.path)
			if err != nil {
				return nil, err
			}
			violations = append(violations, v...)
		} else {
			v, err := checkAIConfigFile(ctx, client, repo, ref, entry.path)
			if err != nil {
				return nil, err
			}
			violations = append(violations, v...)
		}
	}

	if len(violations) > 0 {
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusFail,
			Severity: rule.Severity,
			Message:  fmt.Sprintf("executable hooks found in AI config: %s", strings.Join(violations, "; ")),
		}, nil
	}

	return &compliance.Result{
		RuleID:   rule.ID,
		RuleName: rule.Name,
		Repo:     repo.FullName,
		Status:   compliance.StatusPass,
		Severity: rule.Severity,
		Message:  "no executable hooks in AI config files",
	}, nil
}

// checkAIConfigDir lists a directory and checks each JSON file inside it.
func checkAIConfigDir(ctx context.Context, client *github.Client, repo gh.Repository, ref, dirPath string) ([]string, error) {
	entries, err := gh.ListDirectoryContents(ctx, client, repo.Owner, repo.Name, dirPath, ref)
	if err != nil {
		return nil, err
	}

	var violations []string
	for _, entry := range entries {
		name := entry.GetName()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		path := dirPath + "/" + name
		v, err := checkAIConfigFile(ctx, client, repo, ref, path)
		if err != nil {
			return nil, err
		}
		violations = append(violations, v...)
	}
	return violations, nil
}

// checkAIConfigFile fetches a JSON file and looks for executable hook definitions.
func checkAIConfigFile(ctx context.Context, client *github.Client, repo gh.Repository, ref, path string) ([]string, error) {
	content, err := gh.FetchFileContent(ctx, client, repo.Owner, repo.Name, path, ref)
	if err != nil {
		return nil, err
	}
	if content == nil {
		return nil, nil
	}

	violations := findExecutableHooks(content, path)
	return violations, nil
}

// findExecutableHooks inspects JSON content for hook or command definitions
// that indicate executable code. Returns a slice of violation descriptions.
func findExecutableHooks(content []byte, path string) []string {
	var data any
	if err := json.Unmarshal(content, &data); err != nil {
		// Not valid JSON or not our concern.
		return nil
	}

	var violations []string
	walkForHooks(data, path, "", &violations)
	return violations
}

// walkForHooks recursively traverses a JSON value looking for hook patterns.
// It looks for:
//   - "hooks" keys whose values contain "command" fields (Claude Code hook format)
//   - "mcpServers" keys whose values contain "command" fields (MCP server format)
//   - Top-level "command" keys (direct executable references in .mcp.json)
func walkForHooks(v any, filePath, keyPath string, violations *[]string) {
	switch node := v.(type) {
	case map[string]any:
		for k, val := range node {
			childPath := joinKeyPath(keyPath, k)
			switch k {
			case "hooks":
				if hasCommandField(val) {
					*violations = append(*violations, fmt.Sprintf("%s: executable hook at %q", filePath, childPath))
				}
			case "mcpServers":
				if hasCommandField(val) {
					*violations = append(*violations, fmt.Sprintf("%s: executable MCP server command at %q", filePath, childPath))
				}
			case "command":
				// A bare "command" key at any level is an executable reference.
				if str, ok := val.(string); ok && str != "" {
					*violations = append(*violations, fmt.Sprintf("%s: executable command at %q", filePath, childPath))
				}
			default:
				walkForHooks(val, filePath, childPath, violations)
			}
		}
	case []any:
		for i, item := range node {
			childPath := fmt.Sprintf("%s[%d]", keyPath, i)
			walkForHooks(item, filePath, childPath, violations)
		}
	}
}

// hasCommandField returns true if v (at any nesting depth) contains a non-empty "command" string field.
func hasCommandField(v any) bool {
	switch node := v.(type) {
	case map[string]any:
		if cmd, ok := node["command"]; ok {
			if str, ok := cmd.(string); ok && str != "" {
				return true
			}
		}
		for _, val := range node {
			if hasCommandField(val) {
				return true
			}
		}
	case []any:
		for _, item := range node {
			if hasCommandField(item) {
				return true
			}
		}
	}
	return false
}

// joinKeyPath builds a dotted JSON key path.
func joinKeyPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

func init() {
	compliance.Register(&aiConfigSafetyChecker{})
}
