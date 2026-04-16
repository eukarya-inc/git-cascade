package checks

import (
	"strings"
	"testing"
)

func TestFindExecutableHooks_ClaudeHook(t *testing.T) {
	// Simulates .claude/settings.json with a PostToolUse hook.
	content := []byte(`{
		"hooks": {
			"PostToolUse": [{"command": "echo hello"}]
		}
	}`)
	violations := findExecutableHooks(content, ".claude/settings.json")
	if len(violations) == 0 {
		t.Fatal("expected violation for hooks with command, got none")
	}
	if !strings.Contains(violations[0], "hooks") {
		t.Errorf("unexpected violation message: %s", violations[0])
	}
}

func TestFindExecutableHooks_MCPServer(t *testing.T) {
	// Simulates .mcp.json with an MCP server definition.
	content := []byte(`{
		"mcpServers": {
			"myserver": {"command": "/usr/local/bin/myserver", "args": ["--port", "3000"]}
		}
	}`)
	violations := findExecutableHooks(content, ".mcp.json")
	if len(violations) == 0 {
		t.Fatal("expected violation for mcpServers with command, got none")
	}
	if !strings.Contains(violations[0], "mcpServers") {
		t.Errorf("unexpected violation message: %s", violations[0])
	}
}

func TestFindExecutableHooks_BareCommand(t *testing.T) {
	// Simulates a config file with a bare "command" key.
	content := []byte(`{
		"something": {
			"command": "make build"
		}
	}`)
	violations := findExecutableHooks(content, ".cursor/settings.json")
	if len(violations) == 0 {
		t.Fatal("expected violation for bare command field, got none")
	}
}

func TestFindExecutableHooks_NoHooks(t *testing.T) {
	// A benign config with no executable hooks.
	content := []byte(`{
		"theme": "dark",
		"fontSize": 14,
		"extensions": ["prettier", "eslint"]
	}`)
	violations := findExecutableHooks(content, ".cursor/settings.json")
	if len(violations) != 0 {
		t.Errorf("expected no violations, got: %v", violations)
	}
}

func TestFindExecutableHooks_EmptyCommand(t *testing.T) {
	// A "command" key with an empty string should not flag.
	content := []byte(`{"command": ""}`)
	violations := findExecutableHooks(content, ".mcp.json")
	if len(violations) != 0 {
		t.Errorf("expected no violations for empty command, got: %v", violations)
	}
}

func TestFindExecutableHooks_InvalidJSON(t *testing.T) {
	violations := findExecutableHooks([]byte(`not json`), ".mcp.json")
	if len(violations) != 0 {
		t.Errorf("expected no violations for invalid JSON, got: %v", violations)
	}
}

func TestJoinKeyPath(t *testing.T) {
	tests := []struct {
		parent string
		child  string
		want   string
	}{
		{"", "hooks", "hooks"},
		{"hooks", "PostToolUse", "hooks.PostToolUse"},
		{"a", "b", "a.b"},
		{"a.b", "c", "a.b.c"},
	}
	for _, tt := range tests {
		got := joinKeyPath(tt.parent, tt.child)
		if got != tt.want {
			t.Errorf("joinKeyPath(%q, %q) = %q, want %q", tt.parent, tt.child, got, tt.want)
		}
	}
}

func TestFindExecutableHooks_NestedArray(t *testing.T) {
	// Arrays are walked; a command inside a nested array should flag.
	content := []byte(`{
		"tools": [
			{"command": "run-me"}
		]
	}`)
	violations := findExecutableHooks(content, ".cursor/settings.json")
	if len(violations) == 0 {
		t.Fatal("expected violation for command inside array element, got none")
	}
}

func TestFindExecutableHooks_MCPServerNoCommand(t *testing.T) {
	// mcpServers without a command field must not flag.
	content := []byte(`{
		"mcpServers": {
			"myserver": {"url": "https://example.com"}
		}
	}`)
	violations := findExecutableHooks(content, ".mcp.json")
	if len(violations) != 0 {
		t.Errorf("expected no violations for mcpServers without command, got: %v", violations)
	}
}

func TestFindExecutableHooks_HooksNoCommand(t *testing.T) {
	// A "hooks" key whose value has no command field must not flag.
	content := []byte(`{
		"hooks": {
			"PostToolUse": [{"type": "notification"}]
		}
	}`)
	violations := findExecutableHooks(content, ".claude/settings.json")
	if len(violations) != 0 {
		t.Errorf("expected no violations for hooks without command, got: %v", violations)
	}
}

func TestFindExecutableHooks_NullValue(t *testing.T) {
	// JSON null values should not cause a panic.
	content := []byte(`{"command": null}`)
	violations := findExecutableHooks(content, ".mcp.json")
	if len(violations) != 0 {
		t.Errorf("expected no violations for null command value, got: %v", violations)
	}
}

func TestHasCommandField(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want bool
	}{
		{
			name: "direct command",
			v:    map[string]any{"command": "echo hi"},
			want: true,
		},
		{
			name: "nested command",
			v:    map[string]any{"server": map[string]any{"command": "run"}},
			want: true,
		},
		{
			name: "command in slice",
			v:    []any{map[string]any{"command": "run"}},
			want: true,
		},
		{
			name: "empty command",
			v:    map[string]any{"command": ""},
			want: false,
		},
		{
			name: "no command",
			v:    map[string]any{"theme": "dark"},
			want: false,
		},
	}

	for _, tt := range tests {
		got := hasCommandField(tt.v)
		if got != tt.want {
			t.Errorf("%s: hasCommandField() = %v, want %v", tt.name, got, tt.want)
		}
	}
}
