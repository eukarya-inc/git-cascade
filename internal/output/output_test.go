package output

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
)

var sampleResults = []compliance.Result{
	{RuleID: "readme-exists", RuleName: "README", Repo: "org/api", Status: compliance.StatusPass, Severity: config.SeverityWarning, Message: "found README.md"},
	{RuleID: "branch-protection", RuleName: "Branch Protection", Repo: "org/web", Status: compliance.StatusFail, Severity: config.SeverityError, Message: "not enabled on main"},
	{RuleID: "actions-pinned", RuleName: "Actions Pinned", Repo: "org/api", Status: compliance.StatusSkip, Severity: config.SeverityError, Message: "no workflows"},
}

// --- Table ---

func TestWriteTable_GroupedByRepo(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, sampleResults, Options{Format: FormatTable}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()

	// Both repos appear as section headers
	if !strings.Contains(out, "org/api") {
		t.Error("expected org/api header")
	}
	if !strings.Contains(out, "org/web") {
		t.Error("expected org/web header")
	}
	// Repo column must NOT appear in rows
	if strings.Count(out, "org/api") != 1 {
		t.Error("org/api should appear exactly once (as header, not in rows)")
	}
	// Content checks
	if !strings.Contains(out, "readme-exists") {
		t.Error("expected readme-exists rule")
	}
	if !strings.Contains(out, "not enabled on main") {
		t.Error("expected branch-protection message")
	}
}

func TestWriteTable_SortedAlphabetically(t *testing.T) {
	results := []compliance.Result{
		{RuleID: "r", Repo: "org/z", Status: compliance.StatusPass, Severity: config.SeverityWarning, Message: "ok"},
		{RuleID: "r", Repo: "org/a", Status: compliance.StatusPass, Severity: config.SeverityWarning, Message: "ok"},
	}
	var buf bytes.Buffer
	Write(&buf, results, Options{Format: FormatTable})
	out := buf.String()
	posA := strings.Index(out, "org/a")
	posZ := strings.Index(out, "org/z")
	if posA > posZ {
		t.Error("expected org/a before org/z")
	}
}

func TestWriteTable_TrailingNewline(t *testing.T) {
	var buf bytes.Buffer
	Write(&buf, sampleResults, Options{Format: FormatTable})
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Error("expected trailing newline after table")
	}
}

func TestWriteTable_Empty(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, nil, Options{Format: FormatTable}); err != nil {
		t.Fatalf("Write empty: %v", err)
	}
}

// --- JSON ---

func TestWriteJSON_ValidArray(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, sampleResults, Options{Format: FormatJSON}); err != nil {
		t.Fatalf("Write JSON: %v", err)
	}
	var out []compliance.Result
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(out) != len(sampleResults) {
		t.Errorf("expected %d results, got %d", len(sampleResults), len(out))
	}
}

// --- CSV ---

func TestWriteCSV_HeaderAndRows(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, sampleResults, Options{Format: FormatCSV}); err != nil {
		t.Fatalf("Write CSV: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if lines[0] != "status,severity,rule_id,rule_name,repo,message" {
		t.Errorf("unexpected CSV header: %q", lines[0])
	}
	if len(lines) != 1+len(sampleResults) {
		t.Errorf("expected %d data rows, got %d", len(sampleResults), len(lines)-1)
	}
}

func TestCSVEscape(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"plain", "plain"},
		{"with,comma", `"with,comma"`},
		{`with"quote`, `"with""quote"`},
		{"with\nnewline", "\"with\nnewline\""},
		{"", ""},
	}
	for _, c := range cases {
		got := csvEscape(c.input)
		if got != c.want {
			t.Errorf("csvEscape(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// --- HasFailures ---

func TestHasFailures(t *testing.T) {
	noFail := []compliance.Result{
		{Status: compliance.StatusPass, Severity: config.SeverityError},
		{Status: compliance.StatusFail, Severity: config.SeverityWarning},
	}
	if HasFailures(noFail) {
		t.Error("expected no failures (no error-severity fails)")
	}

	withFail := []compliance.Result{
		{Status: compliance.StatusFail, Severity: config.SeverityError},
	}
	if !HasFailures(withFail) {
		t.Error("expected HasFailures=true")
	}
}

// --- Unknown format ---

func TestWrite_UnknownFormat(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, sampleResults, Options{Format: "xml"})
	if err == nil {
		t.Error("expected error for unknown format")
	}
}

// --- OutputPath ---

func TestWrite_OutputPath(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/results.json"
	var buf bytes.Buffer
	if err := Write(&buf, sampleResults, Options{Format: FormatJSON, OutputPath: path}); err != nil {
		t.Fatalf("Write with OutputPath: %v", err)
	}
	// buf should be empty since output went to the file.
	if buf.Len() != 0 {
		t.Error("expected empty buffer when OutputPath is set")
	}
	// File must contain valid JSON.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}
	var out []compliance.Result
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("invalid JSON in output file: %v", err)
	}
	if len(out) != len(sampleResults) {
		t.Errorf("expected %d results, got %d", len(sampleResults), len(out))
	}
}

func TestWrite_OutputPath_InvalidDir(t *testing.T) {
	var buf bytes.Buffer
	err := Write(&buf, sampleResults, Options{Format: FormatJSON, OutputPath: "/no/such/dir/results.json"})
	if err == nil {
		t.Error("expected error for invalid output path")
	}
}
