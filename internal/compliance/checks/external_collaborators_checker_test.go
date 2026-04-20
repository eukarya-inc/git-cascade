package checks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/google/go-github/v84/github"
)

// newCollabServer creates a minimal httptest.Server that serves
// GET /api/v3/repos/{owner}/{repo}/collaborators with the provided users JSON,
// or returns the given statusCode when it is non-zero (used to simulate 403).
func newCollabServer(t *testing.T, statusCode int, users []map[string]any) (*httptest.Server, *github.Client) {
	t.Helper()

	mux := http.NewServeMux()
	const prefix = "/api/v3/repos/"

	mux.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
		// We only care about the collaborators endpoint.
		// URL shape: /api/v3/repos/{owner}/{repo}/collaborators
		rest := r.URL.Path[len(prefix):]
		parts := splitN(rest, "/", 3)
		if len(parts) < 3 || parts[2] != "collaborators" {
			http.NotFound(w, r)
			return
		}

		if statusCode != 0 {
			http.Error(w, http.StatusText(statusCode), statusCode)
			return
		}

		payload := users
		if payload == nil {
			payload = []map[string]any{}
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			t.Errorf("encoding collaborators response: %v", err)
		}
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := github.NewClient(nil).WithAuthToken("fake-token")
	baseURL := srv.URL + "/"
	client, _ = client.WithEnterpriseURLs(baseURL, baseURL)
	return srv, client
}

// collabUser builds a collaborator map suitable for JSON serialisation.
// Set admin=true to grant admin permission.
func collabUser(login string, admin bool) map[string]any {
	return map[string]any{
		"login": login,
		"permissions": map[string]any{
			"admin":    admin,
			"push":     true,
			"pull":     true,
			"triage":   false,
			"maintain": false,
		},
	}
}

// ——— externalCollaboratorsChecker.Check ——————————————————————————————————————

func TestExternalCollaboratorsChecker_403_Skip(t *testing.T) {
	_, client := newCollabServer(t, http.StatusForbidden, nil)

	c := &externalCollaboratorsChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("external-collaborators"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusSkip {
		t.Errorf("expected skip on 403, got %s: %s", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, "insufficient permissions") {
		t.Errorf("unexpected skip message: %q", result.Message)
	}
}

func TestExternalCollaboratorsChecker_NoCollaborators_Pass(t *testing.T) {
	_, client := newCollabServer(t, 0, []map[string]any{})

	c := &externalCollaboratorsChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("external-collaborators"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass for empty collaborators list, got %s: %s", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, "no external collaborators with admin access") {
		t.Errorf("unexpected pass message: %q", result.Message)
	}
}

func TestExternalCollaboratorsChecker_CollaboratorsWithoutAdmin_Pass(t *testing.T) {
	users := []map[string]any{
		collabUser("alice", false),
		collabUser("bob", false),
	}
	_, client := newCollabServer(t, 0, users)

	c := &externalCollaboratorsChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("external-collaborators"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusPass {
		t.Errorf("expected pass when no collaborator has admin, got %s: %s", result.Status, result.Message)
	}
}

func TestExternalCollaboratorsChecker_OneAdminCollaborator_Fail(t *testing.T) {
	users := []map[string]any{
		collabUser("evil-admin", true),
	}
	_, client := newCollabServer(t, 0, users)

	c := &externalCollaboratorsChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("external-collaborators"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail for admin collaborator, got %s: %s", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, "evil-admin") {
		t.Errorf("expected failure message to name the admin collaborator, got %q", result.Message)
	}
}

func TestExternalCollaboratorsChecker_MultipleCollaborators_OnlyAdminFails(t *testing.T) {
	users := []map[string]any{
		collabUser("regular-user", false),
		collabUser("power-admin", true),
		collabUser("another-regular", false),
	}
	_, client := newCollabServer(t, 0, users)

	c := &externalCollaboratorsChecker{}
	result, err := c.Check(context.Background(), client, pubRepo(), baseRule("external-collaborators"))
	if err != nil {
		t.Fatalf("Check returned unexpected error: %v", err)
	}
	if result.Status != compliance.StatusFail {
		t.Errorf("expected fail when one of many collaborators is admin, got %s: %s", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, "power-admin") {
		t.Errorf("expected failure message to list admin collaborator, got %q", result.Message)
	}
	if strings.Contains(result.Message, "regular-user") {
		t.Errorf("non-admin collaborator should not appear in failure message, got %q", result.Message)
	}
	if strings.Contains(result.Message, "another-regular") {
		t.Errorf("non-admin collaborator should not appear in failure message, got %q", result.Message)
	}
}
