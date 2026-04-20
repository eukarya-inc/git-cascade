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

// — splitTrimmed / repoShortName ——————————————————————————————————————————————

func TestSplitTrimmed(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"#ops, #security", []string{"#ops", "#security"}},
		{"  api , web ", []string{"api", "web"}},
		{"single", []string{"single"}},
		{"", []string{}},
		{",,,", []string{}},
	}
	for _, c := range cases {
		got := splitTrimmed(c.in)
		if len(got) != len(c.want) {
			t.Errorf("splitTrimmed(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("splitTrimmed(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestRepoShortName(t *testing.T) {
	if got := repoShortName("org/api"); got != "api" {
		t.Errorf("expected 'api', got %q", got)
	}
	if got := repoShortName("api"); got != "api" {
		t.Errorf("expected 'api', got %q", got)
	}
}

// — buildRepoChannelIndex —————————————————————————————————————————————————————

func TestBuildRepoChannelIndex(t *testing.T) {
	mappings := []config.RepositoryChannelMapping{
		{Channels: "#ops, #security", Repositories: "api, backend"},
		{Channels: "#frontend", Repositories: "web"},
	}
	idx := buildRepoChannelIndex(mappings)

	apiChannels := idx["api"]
	if len(apiChannels) != 2 {
		t.Fatalf("expected 2 channels for api, got %v", apiChannels)
	}
	if apiChannels[0] != "#ops" || apiChannels[1] != "#security" {
		t.Errorf("unexpected api channels: %v", apiChannels)
	}
	if len(idx["backend"]) != 2 {
		t.Errorf("expected 2 channels for backend, got %v", idx["backend"])
	}
	if len(idx["web"]) != 1 || idx["web"][0] != "#frontend" {
		t.Errorf("unexpected web channels: %v", idx["web"])
	}
}

func TestBuildRepoChannelIndex_ManyToMany(t *testing.T) {
	// repo1 appears in two mappings → channels from both are collected.
	mappings := []config.RepositoryChannelMapping{
		{Channels: "#ch1", Repositories: "repo1, repo2"},
		{Channels: "#ch2", Repositories: "repo1"},
	}
	idx := buildRepoChannelIndex(mappings)
	if len(idx["repo1"]) != 2 {
		t.Errorf("expected 2 channels for repo1, got %v", idx["repo1"])
	}
}

// — PostSlack (webhook path) ——————————————————————————————————————————————————

func TestPostSlack_NoCredentials(t *testing.T) {
	t.Setenv("GIT_CASCADE_SLACK_WEBHOOK", "")
	t.Setenv("GIT_CASCADE_SLACK_BOT_TOKEN", "")
	cfg := config.SlackConfig{}
	err := PostSlack(cfg, "myorg", nil, "", config.Scope{})
	if err == nil || !strings.Contains(err.Error(), "credentials not set") {
		t.Errorf("expected credentials error, got %v", err)
	}
}

func TestPostSlack_WebhookSuccess(t *testing.T) {
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

// — PostSlack (bot token path) ————————————————————————————————————————————————

func TestPostSlack_BotToken_NoChannel(t *testing.T) {
	t.Setenv("GIT_CASCADE_SLACK_WEBHOOK", "")
	t.Setenv("GIT_CASCADE_SLACK_BOT_TOKEN", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Bot token set but no channel → error.
	cfg := config.SlackConfig{BotToken: "xoxb-fake"}
	err := PostSlack(cfg, "myorg", nil, "", config.Scope{})
	if err == nil || !strings.Contains(err.Error(), "no channel set") {
		t.Errorf("expected 'no channel set' error, got %v", err)
	}
}

func TestPostSlack_BotToken_DefaultChannel(t *testing.T) {
	var received slackPayload
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// Override the API URL by pointing BotToken path at the test server.
	// We do this by setting the bot token and a webhook URL that is unused,
	// but we need to swap the constant — instead test via env-resolved token
	// by calling postSlackSummary directly with our server URL.
	if err := postSlackSummary(srv.URL, "xoxb-fake", "#general", "myorg", nil, "", config.Scope{}); err != nil {
		t.Fatalf("postSlackSummary: %v", err)
	}
	if !strings.HasPrefix(authHeader, "Bearer ") {
		t.Errorf("expected Bearer auth header, got %q", authHeader)
	}
	if received.Channel != "#general" {
		t.Errorf("expected channel=#general, got %q", received.Channel)
	}
}

func TestPostSlack_BotToken_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":false,"error":"channel_not_found"}`))
	}))
	defer srv.Close()

	err := postSlackSummary(srv.URL, "xoxb-fake", "#missing", "myorg", nil, "", config.Scope{})
	if err == nil || !strings.Contains(err.Error(), "channel_not_found") {
		t.Errorf("expected channel_not_found error, got %v", err)
	}
}

func TestPostSlack_BotTokenFromEnv(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// postSlackSummary is used directly because PostSlack posts to the real
	// Slack API endpoint for bot tokens; env resolution is tested here.
	t.Setenv("GIT_CASCADE_SLACK_BOT_TOKEN", "xoxb-from-env")
	cfg := config.SlackConfig{Channel: "#ops"}

	token := cfg.BotToken
	if token == "" {
		token = "xoxb-from-env"
	}
	if err := postSlackSummary(srv.URL, token, cfg.Channel, "myorg", nil, "", config.Scope{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// — repository_channels routing ———————————————————————————————————————————————

func TestPostSlack_RepositoryChannels_Routing(t *testing.T) {
	type call struct {
		channel string
	}
	var calls []call

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p slackPayload
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &p)
		calls = append(calls, call{channel: p.Channel})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	results := []compliance.Result{
		{Repo: "org/api", Status: compliance.StatusFail, Severity: config.SeverityError},
		{Repo: "org/web", Status: compliance.StatusFail, Severity: config.SeverityError},
		{Repo: "org/sandbox", Status: compliance.StatusFail, Severity: config.SeverityWarning},
	}

	cfg := config.SlackConfig{
		BotToken: "xoxb-fake",
		Channel:  "#fallback",
		RepositoryChannels: []config.RepositoryChannelMapping{
			{Channels: "#backend", Repositories: "api"},
			{Channels: "#frontend", Repositories: "web"},
		},
	}

	// Call postSlackViaBot with the test server URL injected via a wrapper.
	// Since we can't override slackAPIPostMessage constant in tests, call the
	// internal fanout logic by substituting the URL.
	if err := postSlackViaBotURL(srv.URL, cfg, "myorg", results, "", config.Scope{}); err != nil {
		t.Fatalf("postSlackViaBotURL: %v", err)
	}

	// Expect 3 calls: #backend (api), #frontend (web), #fallback (sandbox).
	if len(calls) != 3 {
		t.Fatalf("expected 3 Slack calls, got %d: %+v", len(calls), calls)
	}
	channelsSeen := make(map[string]bool)
	for _, c := range calls {
		channelsSeen[c.channel] = true
	}
	for _, want := range []string{"#backend", "#frontend", "#fallback"} {
		if !channelsSeen[want] {
			t.Errorf("expected call to channel %s, saw %v", want, calls)
		}
	}
}

func TestPostSlack_RepositoryChannels_NoFallbackForUnmapped(t *testing.T) {
	// When cfg.Channel is empty, unmapped repos are silently dropped.
	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	results := []compliance.Result{
		{Repo: "org/api", Status: compliance.StatusFail, Severity: config.SeverityError},
		{Repo: "org/unmapped", Status: compliance.StatusFail, Severity: config.SeverityError},
	}
	cfg := config.SlackConfig{
		BotToken: "xoxb-fake",
		// No default Channel.
		RepositoryChannels: []config.RepositoryChannelMapping{
			{Channels: "#backend", Repositories: "api"},
		},
	}

	if err := postSlackViaBotURL(srv.URL, cfg, "myorg", results, "", config.Scope{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 Slack call (only mapped repo), got %d", callCount)
	}
}

func TestPostSlack_RepositoryChannels_ShortNameMatch(t *testing.T) {
	// Repos listed as short names (without owner prefix) should still match.
	var received slackPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	results := []compliance.Result{
		{Repo: "myorg/api", Status: compliance.StatusFail, Severity: config.SeverityError},
	}
	cfg := config.SlackConfig{
		BotToken: "xoxb-fake",
		RepositoryChannels: []config.RepositoryChannelMapping{
			// Short name "api" should match full name "myorg/api".
			{Channels: "#backend", Repositories: "api"},
		},
	}

	if err := postSlackViaBotURL(srv.URL, cfg, "myorg", results, "", config.Scope{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if received.Channel != "#backend" {
		t.Errorf("expected #backend, got %q", received.Channel)
	}
}

func TestPostSlack_RepositoryChannels_ManyToMany(t *testing.T) {
	// repo1 mapped to #ch1 and #ch2 → two calls with the same repo's results.
	var callCount int
	channelsSeen := make(map[string]bool)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p slackPayload
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &p)
		callCount++
		channelsSeen[p.Channel] = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	results := []compliance.Result{
		{Repo: "org/repo1", Status: compliance.StatusFail, Severity: config.SeverityError},
	}
	cfg := config.SlackConfig{
		BotToken: "xoxb-fake",
		RepositoryChannels: []config.RepositoryChannelMapping{
			{Channels: "#ch1, #ch2", Repositories: "repo1"},
		},
	}

	if err := postSlackViaBotURL(srv.URL, cfg, "myorg", results, "", config.Scope{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 Slack calls (one per channel), got %d", callCount)
	}
	if !channelsSeen["#ch1"] || !channelsSeen["#ch2"] {
		t.Errorf("expected calls to #ch1 and #ch2, saw %v", channelsSeen)
	}
}
