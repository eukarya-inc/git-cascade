package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStdout redirects os.Stdout for the duration of fn and returns what was written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	r.Close()
	return string(out)
}

// writeYAML writes content to dir/name and returns its full path.
func writeYAML(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return path
}

const validYAML = `
version: "1"
rules:
  - id: readme-exists
    name: README
    description: desc
    severity: warning
    enabled: true
`

// — validate: individual file arguments ———————————————————————————————————————

func TestRunValidate_SingleValidFile(t *testing.T) {
	dir := t.TempDir()
	path := writeYAML(t, dir, "rules.yaml", validYAML)

	var err error
	out := captureStdout(t, func() {
		err = runValidate(nil, []string{path})
	})
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if !strings.Contains(out, "OK") {
		t.Errorf("expected OK in output, got: %q", out)
	}
	if !strings.Contains(out, "rules.yaml") {
		t.Errorf("expected filename in output, got: %q", out)
	}
}

func TestRunValidate_MultipleValidFiles(t *testing.T) {
	dir := t.TempDir()
	p1 := writeYAML(t, dir, "a.yaml", validYAML)
	p2 := writeYAML(t, dir, "b.yaml", `
version: "1"
rules:
  - id: license-exists
    name: License
    description: desc
    severity: error
    enabled: true
`)
	var err error
	out := captureStdout(t, func() {
		err = runValidate(nil, []string{p1, p2})
	})
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}
	if strings.Count(out, "OK") != 2 {
		t.Errorf("expected 2 OK lines, got: %q", out)
	}
}

func TestRunValidate_InvalidFile_MissingVersion(t *testing.T) {
	dir := t.TempDir()
	path := writeYAML(t, dir, "bad.yaml", `
rules:
  - id: readme-exists
    name: README
    severity: warning
    enabled: true
`)
	err := runValidate(nil, []string{path})
	if err == nil {
		t.Error("expected error for missing version")
	}
}

func TestRunValidate_InvalidFile_BadSeverity(t *testing.T) {
	dir := t.TempDir()
	path := writeYAML(t, dir, "bad.yaml", `
version: "1"
rules:
  - id: readme-exists
    name: README
    severity: critical
    enabled: true
`)
	err := runValidate(nil, []string{path})
	if err == nil {
		t.Error("expected error for invalid severity")
	}
}

func TestRunValidate_MissingFile(t *testing.T) {
	err := runValidate(nil, []string{"/no/such/file.yaml"})
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestRunValidate_PartialFailure(t *testing.T) {
	// One file valid, one file invalid → overall error but first file still printed OK.
	dir := t.TempDir()
	good := writeYAML(t, dir, "good.yaml", validYAML)
	bad := writeYAML(t, dir, "bad.yaml", `
rules:
  - id: readme-exists
    name: README
    severity: warning
    enabled: true
`)
	var err error
	out := captureStdout(t, func() {
		err = runValidate(nil, []string{good, bad})
	})
	if err == nil {
		t.Error("expected error when at least one file fails")
	}
	if !strings.Contains(out, "OK") {
		t.Errorf("expected valid file to print OK, got: %q", out)
	}
}

func TestRunValidate_NoArgs(t *testing.T) {
	validateFlags.configDir = ""
	err := runValidate(nil, []string{})
	if err == nil {
		t.Error("expected error when no args and no --local-config flag")
	}
}

// — validate: --local-config path (validateFlags.configDir) ——————————————————

func TestRunValidate_LocalConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "rules.yaml", validYAML)

	validateFlags.configDir = dir
	t.Cleanup(func() { validateFlags.configDir = "" })

	var err error
	out := captureStdout(t, func() {
		err = runValidate(nil, nil)
	})
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}
	if !strings.Contains(out, "OK") {
		t.Errorf("expected OK output, got: %q", out)
	}
}

func TestRunValidate_LocalConfig_Invalid(t *testing.T) {
	dir := t.TempDir()
	// No version anywhere → validation fails.
	writeYAML(t, dir, "rules.yaml", `
rules:
  - id: readme-exists
    name: README
    severity: warning
    enabled: true
`)
	validateFlags.configDir = dir
	t.Cleanup(func() { validateFlags.configDir = "" })

	err := runValidate(nil, nil)
	if err == nil {
		t.Error("expected error for invalid config directory")
	}
}

func TestRunValidate_LocalConfig_MissingDir(t *testing.T) {
	validateFlags.configDir = "/no/such/dir"
	t.Cleanup(func() { validateFlags.configDir = "" })

	err := runValidate(nil, nil)
	if err == nil {
		t.Error("expected error for missing directory")
	}
}

func TestRunValidate_LocalConfig_RuleCount(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "a.yaml", validYAML)
	writeYAML(t, dir, "b.yaml", `
rules:
  - id: license-exists
    name: License
    description: desc
    severity: error
    enabled: true
`)
	validateFlags.configDir = dir
	t.Cleanup(func() { validateFlags.configDir = "" })

	var err error
	out := captureStdout(t, func() {
		err = runValidate(nil, nil)
	})
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}
	// Should report "2 rules" since both files are merged.
	if !strings.Contains(out, "2 rules") {
		t.Errorf("expected '2 rules' in output, got: %q", out)
	}
}
