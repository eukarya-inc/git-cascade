package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
)

func TestSeverityToSARIFLevel(t *testing.T) {
	tests := []struct {
		severity config.Severity
		want     string
	}{
		{config.SeverityError, "error"},
		{config.SeverityWarning, "warning"},
		{config.SeverityInfo, "note"},
		{"unknown", "note"},
		{"", "note"},
	}
	for _, tt := range tests {
		got := severityToSARIFLevel(tt.severity)
		if got != tt.want {
			t.Errorf("severityToSARIFLevel(%q) = %q, want %q", tt.severity, got, tt.want)
		}
	}
}

func TestWriteSARIF_Schema(t *testing.T) {
	var buf bytes.Buffer
	if err := writeSARIF(&buf, sampleResults); err != nil {
		t.Fatalf("writeSARIF: %v", err)
	}

	var log sarifLog
	if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
		t.Fatalf("invalid SARIF JSON: %v", err)
	}
	if log.Schema != sarifSchema {
		t.Errorf("schema = %q, want %q", log.Schema, sarifSchema)
	}
	if log.Version != sarifVersion {
		t.Errorf("version = %q, want %q", log.Version, sarifVersion)
	}
}

func TestWriteSARIF_SingleRun(t *testing.T) {
	var buf bytes.Buffer
	writeSARIF(&buf, sampleResults)

	var log sarifLog
	json.Unmarshal(buf.Bytes(), &log)

	if len(log.Runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(log.Runs))
	}
	driver := log.Runs[0].Tool.Driver
	if driver.Name != "git-cascade" {
		t.Errorf("driver name = %q, want git-cascade", driver.Name)
	}
	if driver.InformationURI == "" {
		t.Error("expected non-empty informationUri")
	}
}

func TestWriteSARIF_OnlyFailuresEmitted(t *testing.T) {
	// sampleResults has 1 pass, 1 fail, 1 skip.
	var buf bytes.Buffer
	writeSARIF(&buf, sampleResults)

	var log sarifLog
	json.Unmarshal(buf.Bytes(), &log)

	results := log.Runs[0].Results
	if len(results) != 1 {
		t.Fatalf("expected 1 SARIF result (only failures), got %d", len(results))
	}
	if results[0].RuleID != "branch-protection" {
		t.Errorf("expected branch-protection failure, got %q", results[0].RuleID)
	}
}

func TestWriteSARIF_DeduplicatedRules(t *testing.T) {
	results := []compliance.Result{
		{RuleID: "r1", RuleName: "R1", Repo: "org/a", Status: compliance.StatusFail, Severity: config.SeverityError, Message: "fail"},
		{RuleID: "r1", RuleName: "R1", Repo: "org/b", Status: compliance.StatusFail, Severity: config.SeverityError, Message: "fail"},
		{RuleID: "r2", RuleName: "R2", Repo: "org/a", Status: compliance.StatusPass, Severity: config.SeverityWarning, Message: "ok"},
	}
	var buf bytes.Buffer
	writeSARIF(&buf, results)

	var log sarifLog
	json.Unmarshal(buf.Bytes(), &log)

	rules := log.Runs[0].Tool.Driver.Rules
	if len(rules) != 2 {
		t.Errorf("expected 2 deduplicated rules, got %d", len(rules))
	}
}

func TestWriteSARIF_LocationContainsRepo(t *testing.T) {
	results := []compliance.Result{
		{RuleID: "r1", RuleName: "R1", Repo: "myorg/myrepo", Status: compliance.StatusFail, Severity: config.SeverityError, Message: "bad"},
	}
	var buf bytes.Buffer
	writeSARIF(&buf, results)

	var log sarifLog
	json.Unmarshal(buf.Bytes(), &log)

	r := log.Runs[0].Results[0]
	uri := r.Locations[0].PhysicalLocation.ArtifactLocation.URI
	if !strings.Contains(uri, "myorg/myrepo") {
		t.Errorf("location URI %q should contain repo name", uri)
	}
	if !strings.Contains(r.Message.Text, "myorg/myrepo") {
		t.Errorf("message text %q should contain repo name", r.Message.Text)
	}
}

func TestWriteSARIF_Empty(t *testing.T) {
	var buf bytes.Buffer
	if err := writeSARIF(&buf, nil); err != nil {
		t.Fatalf("writeSARIF with nil results: %v", err)
	}
	var log sarifLog
	if err := json.Unmarshal(buf.Bytes(), &log); err != nil {
		t.Fatalf("invalid JSON for empty results: %v", err)
	}
	if len(log.Runs[0].Results) != 0 {
		t.Error("expected empty results slice for no input")
	}
}

func TestWriteSARIF_ViaWrite(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, sampleResults, Options{Format: FormatSARIF}); err != nil {
		t.Fatalf("Write SARIF: %v", err)
	}
	if !strings.Contains(buf.String(), sarifVersion) {
		t.Error("expected SARIF version in output")
	}
}

func TestWriteSARIF_RuleLevels(t *testing.T) {
	results := []compliance.Result{
		{RuleID: "e", RuleName: "E", Repo: "org/r", Status: compliance.StatusFail, Severity: config.SeverityError, Message: "x"},
		{RuleID: "w", RuleName: "W", Repo: "org/r", Status: compliance.StatusFail, Severity: config.SeverityWarning, Message: "x"},
		{RuleID: "i", RuleName: "I", Repo: "org/r", Status: compliance.StatusFail, Severity: config.SeverityInfo, Message: "x"},
	}
	var buf bytes.Buffer
	writeSARIF(&buf, results)

	var log sarifLog
	json.Unmarshal(buf.Bytes(), &log)

	rules := log.Runs[0].Tool.Driver.Rules
	ruleMap := map[string]string{}
	for _, r := range rules {
		ruleMap[r.ID] = r.DefaultConfig.Level
	}

	if ruleMap["e"] != "error" {
		t.Errorf("expected error level for severity error, got %q", ruleMap["e"])
	}
	if ruleMap["w"] != "warning" {
		t.Errorf("expected warning level for severity warning, got %q", ruleMap["w"])
	}
	if ruleMap["i"] != "note" {
		t.Errorf("expected note level for severity info, got %q", ruleMap["i"])
	}
}
