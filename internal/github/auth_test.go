package github

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewPATClient(t *testing.T) {
	if _, err := newPATClient(""); err == nil {
		t.Error("expected error for empty token")
	}
	client, err := newPATClient("fake-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func writeTestRSAKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating RSA key: %v", err)
	}
	der := x509.MarshalPKCS1PrivateKey(key)
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}
	path := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("writing key file: %v", err)
	}
	return path
}

func TestNewAppClient(t *testing.T) {
	keyPath := writeTestRSAKey(t)

	if _, err := newAppClient(0, 1, keyPath); err == nil {
		t.Error("expected error for missing appID")
	}
	if _, err := newAppClient(1, 1, ""); err == nil {
		t.Error("expected error for missing privateKeyPath")
	}
	if _, err := newAppClient(1, 1, filepath.Join(t.TempDir(), "missing.pem")); err == nil {
		t.Error("expected error for unreadable key file")
	}

	client, err := newAppClient(1, 2, keyPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

// redirectTransport rewrites outbound requests to target a test server while
// keeping the request path, letting appTransport's client (built with a fixed
// api.github.com base URL) be exercised against an httptest.Server.
type redirectTransport struct {
	target *url.URL
}

func (rt *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = rt.target.Scheme
	req.URL.Host = rt.target.Host
	req.Host = rt.target.Host
	return http.DefaultTransport.RoundTrip(req)
}

func TestAppTransport_GetInstallationToken(t *testing.T) {
	keyPath := writeTestRSAKey(t)
	key, err := loadPrivateKey(keyPath)
	if err != nil {
		t.Fatalf("loading key: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/app/installations/42/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"token":      "installation-token",
			"expires_at": time.Now().Add(time.Hour).Format(time.RFC3339),
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parsing test server URL: %v", err)
	}

	at := &appTransport{
		appID:          1,
		installationID: 42,
		key:            key,
		base:           &redirectTransport{target: target},
	}

	token, err := at.getInstallationToken(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token != "installation-token" {
		t.Errorf("got token %q, want %q", token, "installation-token")
	}

	// Cached token should be reused without another request.
	mux.HandleFunc("/app/installations/42/access_tokens/should-not-be-called", func(w http.ResponseWriter, r *http.Request) {
		t.Error("installation token endpoint should not be re-fetched while cached")
	})
	token2, err := at.getInstallationToken(t.Context())
	if err != nil {
		t.Fatalf("unexpected error on cached fetch: %v", err)
	}
	if token2 != token {
		t.Errorf("expected cached token %q, got %q", token, token2)
	}
}

func TestAppTransport_RoundTrip(t *testing.T) {
	keyPath := writeTestRSAKey(t)
	key, err := loadPrivateKey(keyPath)
	if err != nil {
		t.Fatalf("loading key: %v", err)
	}

	var gotAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/app/installations/7/access_tokens", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"token":      "tok-abc",
			"expires_at": time.Now().Add(time.Hour).Format(time.RFC3339),
		})
	})
	mux.HandleFunc("/some/path", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parsing test server URL: %v", err)
	}

	at := &appTransport{
		appID:          1,
		installationID: 7,
		key:            key,
		base:           &redirectTransport{target: target},
	}

	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/some/path", nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	resp, err := at.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if gotAuth != "token tok-abc" {
		t.Errorf("got Authorization header %q, want %q", gotAuth, "token tok-abc")
	}
}
