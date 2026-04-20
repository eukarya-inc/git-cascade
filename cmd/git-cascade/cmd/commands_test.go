package cmd

import (
	"bytes"
	"strings"
	"testing"
)

// execRootCmd runs rootCmd with args, capturing the cobra-routed output.
// For commands that print via cmd.Print / cmd.Println (cobra output), use this.
// For commands that print directly to os.Stdout, use captureStdout instead.
func execRootCmd(t *testing.T, args []string) (stdout, stderr string, err error) {
	t.Helper()
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	rootCmd.SetOut(outBuf)
	rootCmd.SetErr(errBuf)
	rootCmd.SetArgs(args)
	err = rootCmd.Execute()
	// Reset to defaults so other tests aren't affected.
	rootCmd.SetOut(nil)
	rootCmd.SetErr(nil)
	return outBuf.String(), errBuf.String(), err
}

// — version ——————————————————————————————————————————————————————————————————

func TestVersionCommand_PrintsVersion(t *testing.T) {
	original := Version
	Version = "1.2.3-test"
	t.Cleanup(func() { Version = original })

	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"version"})
		rootCmd.Execute() //nolint:errcheck
	})
	if !strings.Contains(out, "1.2.3-test") {
		t.Errorf("expected version in output, got: %q", out)
	}
}

func TestVersionCommand_ContainsBinaryName(t *testing.T) {
	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"version"})
		rootCmd.Execute() //nolint:errcheck
	})
	if !strings.Contains(out, "git-cascade") {
		t.Errorf("expected 'git-cascade' in version output, got: %q", out)
	}
}

// — completion ————————————————————————————————————————————————————————————————

func TestCompletionCommand_Bash(t *testing.T) {
	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"completion", "bash"})
		rootCmd.Execute() //nolint:errcheck
	})
	if !strings.Contains(out, "bash") {
		t.Errorf("expected bash completion output, got: %q", out[:min(len(out), 100)])
	}
}

func TestCompletionCommand_Zsh(t *testing.T) {
	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"completion", "zsh"})
		rootCmd.Execute() //nolint:errcheck
	})
	if out == "" {
		t.Error("expected non-empty zsh completion output")
	}
}

func TestCompletionCommand_Fish(t *testing.T) {
	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"completion", "fish"})
		rootCmd.Execute() //nolint:errcheck
	})
	if out == "" {
		t.Error("expected non-empty fish completion output")
	}
}

func TestCompletionCommand_Powershell(t *testing.T) {
	out := captureStdout(t, func() {
		rootCmd.SetArgs([]string{"completion", "powershell"})
		rootCmd.Execute() //nolint:errcheck
	})
	if out == "" {
		t.Error("expected non-empty powershell completion output")
	}
}

func TestCompletionCommand_InvalidShell(t *testing.T) {
	// cobra.ExactArgs(1) will catch wrong arg count; an unknown shell falls to Help.
	_, _, err := execRootCmd(t, []string{"completion", "tcsh"})
	// The command falls through to Help, which is not an error from cobra's POV,
	// but the output should include usage/help text.
	_ = err // help exit is not an error
}

func TestCompletionCommand_NoArgs(t *testing.T) {
	_, _, err := execRootCmd(t, []string{"completion"})
	// ExactArgs(1) should return an error.
	if err == nil {
		t.Error("expected error when completion called with no shell argument")
	}
}

// — Execute (root) —————————————————————————————————————————————————————————————

func TestExecute_ReturnsNilOnHelp(t *testing.T) {
	// Calling root with no subcommand shows help and returns nil.
	rootCmd.SetArgs([]string{})
	err := Execute()
	if err != nil {
		t.Errorf("expected nil on root help, got: %v", err)
	}
}

// — runScan early-exit paths ——————————————————————————————————————————————————

func TestRunScan_CredentialError_IncompleteApp(t *testing.T) {
	resetScanFlags()
	t.Setenv("GIT_CASCADE_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GIT_CASCADE_APP_ID", "")
	t.Setenv("GIT_CASCADE_INSTALLATION_ID", "")
	t.Setenv("GIT_CASCADE_PRIVATE_KEY_PATH", "")

	// Provide two of the three App fields (missing private key path) → resolveCredentials error.
	scanFlags.org = "testorg"
	scanFlags.appID = 1
	scanFlags.installationID = 2
	scanFlags.privateKeyPath = ""
	t.Cleanup(func() {
		scanFlags.org = ""
		scanFlags.appID = 0
		scanFlags.installationID = 0
	})

	err := runScan(scanCmd, nil)
	if err == nil {
		t.Error("expected credential error for incomplete App fields")
	}
	if !strings.Contains(err.Error(), "requires") {
		t.Errorf("expected 'requires' in error message, got: %v", err)
	}
}

func TestRunScan_LocalConfigNotFound(t *testing.T) {
	resetScanFlags()
	t.Setenv("GIT_CASCADE_TOKEN", "fake-token-for-test")
	t.Setenv("GIT_CASCADE_APP_ID", "")
	t.Setenv("GIT_CASCADE_INSTALLATION_ID", "")
	t.Setenv("GIT_CASCADE_PRIVATE_KEY_PATH", "")

	scanFlags.org = "testorg"
	scanFlags.localConfig = "/no/such/config/dir"
	t.Cleanup(func() {
		scanFlags.org = ""
		scanFlags.localConfig = ""
	})

	err := runScan(scanCmd, nil)
	if err == nil {
		t.Error("expected error for missing local config directory")
	}
	if !strings.Contains(err.Error(), "loading compliance config") {
		t.Errorf("expected config loading error, got: %v", err)
	}
}
