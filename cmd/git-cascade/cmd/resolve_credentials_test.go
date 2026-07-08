package cmd

import (
	"strings"
	"testing"

	gh "github.com/eukarya-inc/git-cascade/internal/github"
)

// resetScanFlags zeroes out the fields that resolveCredentials reads so tests
// are hermetically isolated from each other.
func resetScanFlags() {
	scanFlags.token = ""
	scanFlags.appID = 0
	scanFlags.installationID = 0
	scanFlags.privateKeyPath = ""
	scanFlags.notifyToken = ""
	scanFlags.notifyAppID = 0
	scanFlags.notifyInstallationID = 0
	scanFlags.notifyPrivateKeyPath = ""
}

// TestResolveCredentials_PATFlag verifies that a non-empty --token flag
// immediately returns a PAT credential without consulting env vars.
func TestResolveCredentials_PATFlag(t *testing.T) {
	resetScanFlags()
	scanFlags.token = "ghp_test_token"

	creds, err := resolveCredentials()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.Method != gh.AuthPAT {
		t.Errorf("expected AuthPAT, got %v", creds.Method)
	}
	if creds.Token != "ghp_test_token" {
		t.Errorf("expected token ghp_test_token, got %q", creds.Token)
	}
}

// TestResolveCredentials_AppFlag verifies that all three app fields provided via
// flags returns a GitHub App credential.
func TestResolveCredentials_AppFlag(t *testing.T) {
	resetScanFlags()
	t.Setenv("GIT_CASCADE_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	scanFlags.appID = 123
	scanFlags.installationID = 456
	scanFlags.privateKeyPath = "/tmp/key.pem"

	creds, err := resolveCredentials()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.Method != gh.AuthGitHubApp {
		t.Errorf("expected AuthGitHubApp, got %v", creds.Method)
	}
	if creds.AppID != 123 {
		t.Errorf("expected AppID=123, got %d", creds.AppID)
	}
	if creds.InstallationID != 456 {
		t.Errorf("expected InstallationID=456, got %d", creds.InstallationID)
	}
	if creds.PrivateKeyPath != "/tmp/key.pem" {
		t.Errorf("expected PrivateKeyPath=/tmp/key.pem, got %q", creds.PrivateKeyPath)
	}
}

// TestResolveCredentials_AppEnv verifies GitHub App auth assembled entirely from
// env vars.
func TestResolveCredentials_AppEnv(t *testing.T) {
	resetScanFlags()
	t.Setenv("GIT_CASCADE_APP_ID", "789")
	t.Setenv("GIT_CASCADE_INSTALLATION_ID", "101")
	t.Setenv("GIT_CASCADE_PRIVATE_KEY_PATH", "/etc/key.pem")
	t.Setenv("GIT_CASCADE_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	creds, err := resolveCredentials()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.Method != gh.AuthGitHubApp {
		t.Errorf("expected AuthGitHubApp, got %v", creds.Method)
	}
	if creds.AppID != 789 {
		t.Errorf("expected AppID=789, got %d", creds.AppID)
	}
}

// TestResolveCredentials_AppPartialMissingPrivateKey verifies that providing
// only app-id and installation-id (no private key) returns an error.
func TestResolveCredentials_AppPartialMissingPrivateKey(t *testing.T) {
	resetScanFlags()
	t.Setenv("GIT_CASCADE_PRIVATE_KEY_PATH", "")
	scanFlags.appID = 1
	scanFlags.installationID = 2
	// privateKeyPath left empty

	_, err := resolveCredentials()
	if err == nil {
		t.Error("expected error for incomplete App credentials")
	}
}

// TestResolveCredentials_AppPartialMissingInstallation verifies that having
// app-id + key but no installation-id returns an error.
func TestResolveCredentials_AppPartialMissingInstallation(t *testing.T) {
	resetScanFlags()
	t.Setenv("GIT_CASCADE_INSTALLATION_ID", "")
	scanFlags.appID = 1
	scanFlags.privateKeyPath = "/tmp/key.pem"
	// installationID left 0

	_, err := resolveCredentials()
	if err == nil {
		t.Error("expected error for missing installation ID")
	}
}

// TestResolveCredentials_AppPartialMissingAppID verifies that having
// installation-id + key but no app-id returns an error.
func TestResolveCredentials_AppPartialMissingAppID(t *testing.T) {
	resetScanFlags()
	t.Setenv("GIT_CASCADE_APP_ID", "")
	scanFlags.installationID = 2
	scanFlags.privateKeyPath = "/tmp/key.pem"
	// appID left 0

	_, err := resolveCredentials()
	if err == nil {
		t.Error("expected error for missing App ID")
	}
}

// TestResolveCredentials_AppEnvPartialMissingInstallation verifies that only
// setting GIT_CASCADE_APP_ID (no installation, no key) via env errors.
func TestResolveCredentials_AppEnvPartialMissingInstallation(t *testing.T) {
	resetScanFlags()
	t.Setenv("GIT_CASCADE_APP_ID", "42")
	t.Setenv("GIT_CASCADE_INSTALLATION_ID", "")
	t.Setenv("GIT_CASCADE_PRIVATE_KEY_PATH", "")
	t.Setenv("GIT_CASCADE_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	_, err := resolveCredentials()
	if err == nil {
		t.Error("expected error for partial App env (only app-id set)")
	}
}

// TestResolveCredentials_PATFromEnv verifies that GIT_CASCADE_TOKEN is picked
// up when no flags are set.
func TestResolveCredentials_PATFromEnv(t *testing.T) {
	resetScanFlags()
	t.Setenv("GIT_CASCADE_TOKEN", "env_token_value")
	t.Setenv("GIT_CASCADE_APP_ID", "")
	t.Setenv("GIT_CASCADE_INSTALLATION_ID", "")
	t.Setenv("GIT_CASCADE_PRIVATE_KEY_PATH", "")

	creds, err := resolveCredentials()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.Token != "env_token_value" {
		t.Errorf("expected env token, got %q", creds.Token)
	}
}

// TestResolveCredentials_AppIDEnvInvalidFormat verifies that a non-numeric
// GIT_CASCADE_APP_ID env var is silently ignored (falls through to PAT/env path).
func TestResolveCredentials_AppIDEnvInvalidFormat(t *testing.T) {
	resetScanFlags()
	t.Setenv("GIT_CASCADE_APP_ID", "not-a-number")
	t.Setenv("GIT_CASCADE_INSTALLATION_ID", "")
	t.Setenv("GIT_CASCADE_PRIVATE_KEY_PATH", "")
	t.Setenv("GIT_CASCADE_TOKEN", "fallback_token")
	t.Setenv("GITHUB_TOKEN", "")

	creds, err := resolveCredentials()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Invalid app-id is ignored; falls back to PAT from env.
	if creds.Token != "fallback_token" {
		t.Errorf("expected fallback_token, got %q", creds.Token)
	}
}

// TestResolveNotifyCredentials_FallsBackToScanCreds verifies that with no
// --notify-* flag or env var set, resolveNotifyCredentials returns the scan
// credentials unchanged.
func TestResolveNotifyCredentials_FallsBackToScanCreds(t *testing.T) {
	resetScanFlags()
	t.Setenv("GIT_CASCADE_NOTIFY_TOKEN", "")
	t.Setenv("GIT_CASCADE_NOTIFY_APP_ID", "")
	t.Setenv("GIT_CASCADE_NOTIFY_INSTALLATION_ID", "")
	t.Setenv("GIT_CASCADE_NOTIFY_PRIVATE_KEY_PATH", "")

	scanCreds := gh.Credentials{Method: gh.AuthPAT, Token: "scan_token"}
	creds, err := resolveNotifyCredentials(scanCreds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds != scanCreds {
		t.Errorf("expected scan creds unchanged, got %+v", creds)
	}
}

// TestResolveNotifyCredentials_PATFlagOverrides verifies that --notify-token
// takes priority over falling back to scan credentials.
func TestResolveNotifyCredentials_PATFlagOverrides(t *testing.T) {
	resetScanFlags()
	scanFlags.notifyToken = "notify_token"

	scanCreds := gh.Credentials{Method: gh.AuthPAT, Token: "scan_token"}
	creds, err := resolveNotifyCredentials(scanCreds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.Method != gh.AuthPAT || creds.Token != "notify_token" {
		t.Errorf("expected notify_token to override scan creds, got %+v", creds)
	}
}

// TestResolveNotifyCredentials_EnvOverrides verifies GIT_CASCADE_NOTIFY_TOKEN
// takes priority over falling back to scan credentials.
func TestResolveNotifyCredentials_EnvOverrides(t *testing.T) {
	resetScanFlags()
	t.Setenv("GIT_CASCADE_NOTIFY_TOKEN", "notify_env_token")

	scanCreds := gh.Credentials{Method: gh.AuthPAT, Token: "scan_token"}
	creds, err := resolveNotifyCredentials(scanCreds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.Token != "notify_env_token" {
		t.Errorf("expected notify_env_token, got %q", creds.Token)
	}
}

// TestResolveNotifyCredentials_AppFlagsIndependentOfScan verifies a full
// GitHub App identity can be supplied for notify independent of scan's method.
func TestResolveNotifyCredentials_AppFlagsIndependentOfScan(t *testing.T) {
	resetScanFlags()
	scanFlags.notifyAppID = 111
	scanFlags.notifyInstallationID = 222
	scanFlags.notifyPrivateKeyPath = "/tmp/notify-key.pem"

	scanCreds := gh.Credentials{Method: gh.AuthPAT, Token: "scan_token"}
	creds, err := resolveNotifyCredentials(scanCreds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.Method != gh.AuthGitHubApp {
		t.Errorf("expected AuthGitHubApp, got %v", creds.Method)
	}
	if creds.AppID != 111 || creds.InstallationID != 222 || creds.PrivateKeyPath != "/tmp/notify-key.pem" {
		t.Errorf("expected notify App identity, got %+v", creds)
	}
}

// TestResolveNotifyCredentials_PartialAppFlagsError verifies an incomplete
// --notify-* App identity (missing installation ID) surfaces an error that
// names the --notify- flags, not the scan flags.
func TestResolveNotifyCredentials_PartialAppFlagsError(t *testing.T) {
	resetScanFlags()
	scanFlags.notifyAppID = 111
	scanFlags.notifyPrivateKeyPath = "/tmp/notify-key.pem"
	// notifyInstallationID left 0

	scanCreds := gh.Credentials{Method: gh.AuthPAT, Token: "scan_token"}
	_, err := resolveNotifyCredentials(scanCreds)
	if err == nil {
		t.Fatal("expected error for incomplete notify App credentials")
	}
	if !strings.Contains(err.Error(), "--notify-") {
		t.Errorf("expected error to reference --notify- flags, got: %v", err)
	}
}
