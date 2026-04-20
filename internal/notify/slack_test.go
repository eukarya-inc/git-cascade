package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
)

// — countResults ——————————————————————————————————————————————————————————————

func TestCountResults(t *testing.T) {
	results := []compliance.Result{
		{Status: compliance.StatusPass, Severity: config.SeverityError},
		{Status: compliance.StatusFail, Severity: config.SeverityError},
		{Status: compliance.StatusFail, Severity: config.SeverityWarning},
		{Status: compliance.StatusFail, Severity: config.SeverityInfo},
		{Status: compliance.StatusSkip, Severity: config.SeverityWarning},
	}
	passes, warnings, errors := countResults(results)
	// pass: StatusPass(1) + StatusSkip(1) + StatusFail/Info(1) = 3
	if passes != 3 {
		t.Errorf("passes = %d, want 3", passes)
	}
	if warnings != 1 {
		t.Errorf("warnings = %d, want 1", warnings)
	}
	if errors != 1 {
		t.Errorf("errors = %d, want 1", errors)
	}
}

func TestCountResults_Empty(t *testing.T) {
	p, w, e := countResults(nil)
	if p != 0 || w != 0 || e != 0 {
		t.Errorf("expected all zeros, got passes=%d warnings=%d errors=%d", p, w, e)
	}
}

func TestCountResults_AllPass(t *testing.T) {
	results := []compliance.Result{
		{Status: compliance.StatusPass, Severity: config.SeverityError},
		{Status: compliance.StatusSkip, Severity: config.SeverityWarning},
	}
	p, w, e := countResults(results)
	if p != 2 || w != 0 || e != 0 {
		t.Errorf("expected 2 passes, got passes=%d warnings=%d errors=%d", p, w, e)
	}
}

// — groupByRepo ———————————————————————————————————————————————————————————————

func TestGroupByRepo(t *testing.T) {
	results := []compliance.Result{
		{Repo: "org/a", Status: compliance.StatusPass, Severity: config.SeverityWarning},
		{Repo: "org/b", Status: compliance.StatusFail, Severity: config.SeverityError},
		{Repo: "org/a", Status: compliance.StatusFail, Severity: config.SeverityError},
	}
	grouped := groupByRepo(results)
	if len(grouped["org/a"]) != 2 {
		t.Errorf("expected 2 results for org/a, got %d", len(grouped["org/a"]))
	}
	if len(grouped["org/b"]) != 1 {
		t.Errorf("expected 1 result for org/b, got %d", len(grouped["org/b"]))
	}
}

func TestGroupByRepo_Empty(t *testing.T) {
	if got := groupByRepo(nil); len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
}

// — PostSlack —————————————————————————————————————————————————————————————————

func TestPostSlack_NoWebhookURL(t *testing.T) {
	t.Setenv("GIT_CASCADE_SLACK_WEBHOOK", "")
	cfg := config.SlackConfig{}
	err := PostSlack(cfg, "myorg", nil, "", config.Scope{})
	if err == nil || !strings.Contains(err.Error(), "webhook URL not set") {
		t.Errorf("expected webhook URL error, got %v", err)
	}
}

func TestPostSlack_Success(t *testing.T) {
	var received slackPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	results := []compliance.Result{
		{Repo: "org/api", Status: compliance.StatusFail, Severity: config.SeverityError, Message: "bad"},
		{Repo: "org/api", Status: compliance.StatusPass, Severity: config.SeverityWarning, Message: "ok"},
	}
	cfg := config.SlackConfig{WebhookURL: srv.URL}
	if err := PostSlack(cfg, "myorg", results, "", config.Scope{}); err != nil {
		t.Fatalf("PostSlack: %v", err)
	}
	if received.Text == "" {
		t.Error("expected non-empty slack text")
	}
	if len(received.Blocks) == 0 {
		t.Error("expected slack blocks")
	}
}

func TestPostSlack_WithResultsURL(t *testing.T) {
	var received slackPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := config.SlackConfig{WebhookURL: srv.URL}
	if err := PostSlack(cfg, "myorg", nil, "https://ci.example.com/run/1", config.Scope{}); err != nil {
		t.Fatalf("PostSlack: %v", err)
	}
	// The results URL should appear in the section block text.
	found := false
	for _, b := range received.Blocks {
		if b.Text != nil && strings.Contains(b.Text.Text, "ci.example.com") {
			found = true
		}
	}
	if !found {
		t.Error("expected results URL in Slack block text")
	}
}

func TestPostSlack_ResultsURLFromEnv(t *testing.T) {
	t.Setenv("GIT_CASCADE_SLACK_RESULTS_URL", "https://env-url.example.com")
	var received slackPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := config.SlackConfig{WebhookURL: srv.URL}
	if err := PostSlack(cfg, "myorg", nil, "", config.Scope{}); err != nil {
		t.Fatalf("PostSlack: %v", err)
	}
	found := false
	for _, b := range received.Blocks {
		if b.Text != nil && strings.Contains(b.Text.Text, "env-url.example.com") {
			found = true
		}
	}
	if !found {
		t.Error("expected env results URL in Slack block text")
	}
}

func TestPostSlack_WebhookFromEnv(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	t.Setenv("GIT_CASCADE_SLACK_WEBHOOK", srv.URL)

	cfg := config.SlackConfig{} // no WebhookURL set
	if err := PostSlack(cfg, "myorg", nil, "", config.Scope{}); err != nil {
		t.Fatalf("PostSlack with env webhook: %v", err)
	}
}

func TestPostSlack_Non200Response(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := config.SlackConfig{WebhookURL: srv.URL}
	err := PostSlack(cfg, "myorg", nil, "", config.Scope{})
	if err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Errorf("expected status 500 error, got %v", err)
	}
}

func TestPostSlack_WithChannel(t *testing.T) {
	var received slackPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := config.SlackConfig{WebhookURL: srv.URL, Channel: "#compliance"}
	PostSlack(cfg, "myorg", nil, "", config.Scope{})
	if received.Channel != "#compliance" {
		t.Errorf("expected channel=#compliance, got %q", received.Channel)
	}
}

func TestPostSlack_SingleRepo(t *testing.T) {
	var received slackPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	results := []compliance.Result{
		{Repo: "org/api", Status: compliance.StatusPass, Severity: config.SeverityWarning},
	}
	cfg := config.SlackConfig{WebhookURL: srv.URL}
	PostSlack(cfg, "myorg", results, "", config.Scope{})

	// "repository" (singular) should appear when there is exactly one repo.
	found := false
	for _, b := range received.Blocks {
		if b.Text != nil && strings.Contains(b.Text.Text, "repository") {
			found = true
		}
	}
	if !found {
		t.Error("expected singular 'repository' wording for single repo scan")
	}
}
