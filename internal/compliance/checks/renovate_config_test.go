package checks

import (
	"encoding/json"
	"strings"
	"testing"
)

// renovateValidate replicates the checker's JSON validation logic so we can
// test it without a live GitHub client.
func renovateValidate(content []byte, requiredExtends, _ string) (pass bool, message string) {
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(content, &cfg); err != nil {
		return false, "invalid JSON"
	}

	var failures []string

	extendsRaw, ok := cfg["extends"]
	if !ok {
		failures = append(failures, "missing 'extends'")
	} else {
		var extends []string
		if err := json.Unmarshal(extendsRaw, &extends); err != nil {
			failures = append(failures, "'extends' is not an array")
		} else {
			found := false
			for _, e := range extends {
				if strings.Contains(e, requiredExtends) {
					found = true
					break
				}
			}
			if !found {
				failures = append(failures, "extends does not include "+requiredExtends)
			}
		}
	}

	if len(failures) > 0 {
		return false, strings.Join(failures, "; ")
	}
	return true, ""
}

func TestRenovateValidate_Pass(t *testing.T) {
	content := []byte(`{"extends":["github>reearth/renovate-config"]}`)
	pass, msg := renovateValidate(content, "github>reearth/renovate-config", "7")
	if !pass {
		t.Errorf("expected pass, got failure: %s", msg)
	}
}

func TestRenovateValidate_MissingExtends(t *testing.T) {
	content := []byte(`{"stabilityDays":7}`)
	pass, msg := renovateValidate(content, "github>reearth/renovate-config", "7")
	if pass {
		t.Error("expected failure for missing extends")
	}
	if !strings.Contains(msg, "missing 'extends'") {
		t.Errorf("unexpected message: %s", msg)
	}
}

func TestRenovateValidate_WrongPreset(t *testing.T) {
	content := []byte(`{"extends":["config:base"]}`)
	pass, msg := renovateValidate(content, "github>reearth/renovate-config", "7")
	if pass {
		t.Error("expected failure for wrong preset")
	}
	if !strings.Contains(msg, "does not include") {
		t.Errorf("unexpected message: %s", msg)
	}
}

func TestRenovateValidate_InvalidJSON(t *testing.T) {
	content := []byte(`not json`)
	pass, msg := renovateValidate(content, "github>reearth/renovate-config", "7")
	if pass {
		t.Error("expected failure for invalid JSON")
	}
	if !strings.Contains(msg, "invalid JSON") {
		t.Errorf("unexpected message: %s", msg)
	}
}

func TestRenovateValidate_ExtendsNotArray(t *testing.T) {
	content := []byte(`{"extends":"github>reearth/renovate-config"}`)
	pass, msg := renovateValidate(content, "github>reearth/renovate-config", "7")
	if pass {
		t.Error("expected failure when extends is a string not array")
	}
	if !strings.Contains(msg, "not an array") {
		t.Errorf("unexpected message: %s", msg)
	}
}
