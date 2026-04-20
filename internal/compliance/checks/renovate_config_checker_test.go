package checks

import (
	"context"
	"strings"
	"testing"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
)

// ——— renovateConfigChecker.Check —————————————————————————————————————————————

func TestRenovateConfigChecker_NoConfigFound_Fail(t *testing.T) {
	fake := newFakeGitHub()
	// No renovate config registered at any of the known paths.
	_, client := fake.serve(t)

	c := &renovateConfigChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("renovate-config"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail when no config found, got %s: %s", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, "no renovate configuration found") {
		t.Errorf("unexpected failure message: %q", result.Message)
	}
}

func TestRenovateConfigChecker_RenovateJson_Pass(t *testing.T) {
	fake := newFakeGitHub()
	fake.setFile("org", "repo", "renovate.json", []byte(`{
		"extends": ["github>reearth/renovate-config"],
		"stabilityDays": 7
	}`))
	_, client := fake.serve(t)

	c := &renovateConfigChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("renovate-config"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass for valid renovate.json, got %s: %s", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, "renovate.json") {
		t.Errorf("expected message to name the path, got %q", result.Message)
	}
}

func TestRenovateConfigChecker_FallbackPath_GithubDir_Pass(t *testing.T) {
	fake := newFakeGitHub()
	// renovate.json and renovate.json5 are absent; config lives at .github/renovate.json.
	fake.setFile("org", "repo", ".github/renovate.json", []byte(`{
		"extends": ["github>reearth/renovate-config"]
	}`))
	_, client := fake.serve(t)

	c := &renovateConfigChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("renovate-config"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass for .github/renovate.json, got %s: %s", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, ".github/renovate.json") {
		t.Errorf("expected message to name the fallback path, got %q", result.Message)
	}
}

func TestRenovateConfigChecker_InvalidJSON_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setFile("org", "repo", "renovate.json", []byte(`not valid json`))
	_, client := fake.serve(t)

	c := &renovateConfigChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("renovate-config"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail for invalid JSON, got %s: %s", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, "invalid JSON") {
		t.Errorf("expected message to mention invalid JSON, got %q", result.Message)
	}
	if !strings.Contains(result.Message, "renovate.json") {
		t.Errorf("expected message to name the path, got %q", result.Message)
	}
}

func TestRenovateConfigChecker_MissingExtends_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setFile("org", "repo", "renovate.json", []byte(`{
		"stabilityDays": 7
	}`))
	_, client := fake.serve(t)

	c := &renovateConfigChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("renovate-config"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail when extends is missing, got %s: %s", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, "missing 'extends'") {
		t.Errorf("expected message to mention missing extends, got %q", result.Message)
	}
}

func TestRenovateConfigChecker_ExtendsDoesNotIncludePreset_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setFile("org", "repo", "renovate.json", []byte(`{
		"extends": ["config:base"]
	}`))
	_, client := fake.serve(t)

	c := &renovateConfigChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("renovate-config"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail when required preset absent, got %s: %s", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, "does not include") {
		t.Errorf("expected message to mention missing preset, got %q", result.Message)
	}
}

func TestRenovateConfigChecker_StabilityDaysTooLow_Fail(t *testing.T) {
	fake := newFakeGitHub()
	fake.setFile("org", "repo", "renovate.json", []byte(`{
		"extends": ["github>reearth/renovate-config"],
		"stabilityDays": 3
	}`))
	_, client := fake.serve(t)

	c := &renovateConfigChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("renovate-config"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail for stabilityDays=3 < 7, got %s: %s", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, "stabilityDays=3") {
		t.Errorf("expected message to report stabilityDays value, got %q", result.Message)
	}
}

func TestRenovateConfigChecker_StabilityDaysSufficient_Pass(t *testing.T) {
	fake := newFakeGitHub()
	fake.setFile("org", "repo", "renovate.json", []byte(`{
		"extends": ["github>reearth/renovate-config"],
		"stabilityDays": 14
	}`))
	_, client := fake.serve(t)

	c := &renovateConfigChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("renovate-config"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass for stabilityDays=14, got %s: %s", result.Status, result.Message)
	}
}

func TestRenovateConfigChecker_MinimumReleaseAgeContainsDays_Pass(t *testing.T) {
	fake := newFakeGitHub()
	fake.setFile("org", "repo", "renovate.json", []byte(`{
		"extends": ["github>reearth/renovate-config"],
		"minimumReleaseAge": "7 days"
	}`))
	_, client := fake.serve(t)

	c := &renovateConfigChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("renovate-config"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass when minimumReleaseAge contains required days, got %s: %s", result.Status, result.Message)
	}
}

func TestRenovateConfigChecker_CustomExtends_Pass(t *testing.T) {
	fake := newFakeGitHub()
	fake.setFile("org", "repo", "renovate.json", []byte(`{
		"extends": ["github>myorg/my-renovate-config"]
	}`))
	_, client := fake.serve(t)

	rule := baseRule("renovate-config")
	rule.Params = map[string]string{
		"extends": "github>myorg/my-renovate-config",
	}

	c := &renovateConfigChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), rule)
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass with custom extends param, got %s: %s", result.Status, result.Message)
	}
}

func TestRenovateConfigChecker_CustomMinStabilityDays_Fail(t *testing.T) {
	fake := newFakeGitHub()
	// stabilityDays=7 but custom minimum is 14.
	fake.setFile("org", "repo", "renovate.json", []byte(`{
		"extends": ["github>reearth/renovate-config"],
		"stabilityDays": 7
	}`))
	_, client := fake.serve(t)

	rule := baseRule("renovate-config")
	rule.Params = map[string]string{
		"min_stability_days": "14",
	}

	c := &renovateConfigChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), rule)
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail when stabilityDays < custom min_stability_days, got %s: %s", result.Status, result.Message)
	}
}

func TestRenovateConfigChecker_NeitherStabilityField_Pass(t *testing.T) {
	// When neither stabilityDays nor minimumReleaseAge is present, the extends
	// preset may already configure stability; the checker should not fail on that alone.
	fake := newFakeGitHub()
	fake.setFile("org", "repo", "renovate.json", []byte(`{
		"extends": ["github>reearth/renovate-config"]
	}`))
	_, client := fake.serve(t)

	c := &renovateConfigChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("renovate-config"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass when neither stability field is set, got %s: %s", result.Status, result.Message)
	}
}

// Ensure rule.Params is always initialised by baseRule so we can override freely.
var _ config.Rule = baseRule("x")
