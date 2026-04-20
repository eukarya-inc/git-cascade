package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
)

const slackAPIPostMessage = "https://slack.com/api/chat.postMessage"

// slackPayload is a minimal Slack Incoming Webhook payload.
type slackPayload struct {
	Channel string       `json:"channel,omitempty"`
	Text    string       `json:"text"`
	Blocks  []slackBlock `json:"blocks,omitempty"`
}

type slackBlock struct {
	Type string     `json:"type"`
	Text *slackText `json:"text,omitempty"`
}

type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// slackAPIResponse is the envelope returned by the Slack Web API.
type slackAPIResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

// PostSlack sends scan results to one or more Slack channels.
//
// Delivery method (in priority order):
//  1. Bot token (cfg.BotToken or GIT_CASCADE_SLACK_BOT_TOKEN) — uses the Slack
//     Web API (chat.postMessage) and supports per-channel routing via
//     cfg.RepositoryChannels.
//  2. Incoming Webhook (cfg.WebhookURL or GIT_CASCADE_SLACK_WEBHOOK) — sends a
//     single summary; cfg.Channel optionally overrides the webhook's default channel.
//
// When cfg.RepositoryChannels is configured, results are partitioned by repo and
// each channel receives only the results for the repositories mapped to it.
// Repositories not covered by any mapping fall back to cfg.Channel (if set).
//
// resultsURL is an optional run URL linked in the notification; supply an empty
// string to omit it.
func PostSlack(cfg config.SlackConfig, org string, results []compliance.Result, resultsURL string, scope config.Scope) error {
	botToken := cfg.BotToken
	if botToken == "" {
		botToken = os.Getenv("GIT_CASCADE_SLACK_BOT_TOKEN")
	}

	webhookURL := cfg.WebhookURL
	if webhookURL == "" {
		webhookURL = os.Getenv("GIT_CASCADE_SLACK_WEBHOOK")
	}

	if botToken == "" && webhookURL == "" {
		return fmt.Errorf("slack credentials not set (use notify.slack.bot_token / GIT_CASCADE_SLACK_BOT_TOKEN or notify.slack.webhook_url / GIT_CASCADE_SLACK_WEBHOOK)")
	}

	if resultsURL == "" {
		resultsURL = os.Getenv("GIT_CASCADE_SLACK_RESULTS_URL")
	}

	if botToken != "" {
		return postSlackViaBot(cfg, org, results, resultsURL, scope)
	}
	// Webhook path: single summary, no per-repo routing.
	return postSlackSummary(webhookURL, "", cfg.Channel, org, results, resultsURL, scope, false)
}

// postSlackViaBot fans results out to per-channel payloads using the Slack Web API.
func postSlackViaBot(cfg config.SlackConfig, org string, results []compliance.Result, resultsURL string, scope config.Scope) error {
	return postSlackViaBotURL(slackAPIPostMessage, cfg, org, results, resultsURL, scope)
}

// postSlackViaBotURL is the testable core of postSlackViaBot; apiURL can be
// overridden in tests to point at a local httptest server.
func postSlackViaBotURL(apiURL string, cfg config.SlackConfig, org string, results []compliance.Result, resultsURL string, scope config.Scope) error {
	botToken := cfg.BotToken
	if botToken == "" {
		botToken = os.Getenv("GIT_CASCADE_SLACK_BOT_TOKEN")
	}

	if len(cfg.RepositoryChannels) == 0 {
		// No routing configured — send everything to the default channel.
		if cfg.Channel == "" {
			return fmt.Errorf("slack bot token configured but no channel set (set notify.slack.channel or add repository_channels)")
		}
		return postSlackSummary(apiURL, botToken, cfg.Channel, org, results, resultsURL, scope, false)
	}

	repoChannels := buildRepoChannelIndex(cfg.RepositoryChannels)

	// Partition results: channel → results for that channel.
	channelResults := make(map[string][]compliance.Result)
	var unmapped []compliance.Result

	for _, r := range results {
		channels, ok := repoChannels[r.Repo]
		if !ok {
			channels, ok = repoChannels[repoShortName(r.Repo)]
		}
		if !ok {
			unmapped = append(unmapped, r)
			continue
		}
		for _, ch := range channels {
			channelResults[ch] = append(channelResults[ch], r)
		}
	}

	for ch, chResults := range channelResults {
		if err := postSlackSummary(apiURL, botToken, ch, org, chResults, resultsURL, scope, true); err != nil {
			return err
		}
	}

	// Unmapped repos fall back to the default channel — summary only, no repo list.
	if len(unmapped) > 0 && cfg.Channel != "" {
		if err := postSlackSummary(apiURL, botToken, cfg.Channel, org, unmapped, resultsURL, scope, false); err != nil {
			return err
		}
	}

	return nil
}

// buildRepoChannelIndex converts RepositoryChannels into a repo-name → channels map.
func buildRepoChannelIndex(mappings []config.RepositoryChannelMapping) map[string][]string {
	index := make(map[string][]string)
	for _, m := range mappings {
		channels := splitTrimmed(m.Channels)
		repos := splitTrimmed(m.Repositories)
		for _, repo := range repos {
			index[repo] = append(index[repo], channels...)
		}
	}
	return index
}

// postSlackSummary sends a single summary payload.
// When token is non-empty the request is sent as a bot via the Web API (Bearer
// auth); otherwise it is sent as an Incoming Webhook POST with no auth header.
func postSlackSummary(url, token, channel, org string, results []compliance.Result, resultsURL string, scope config.Scope, listRepos bool) error {
	passes, warnings, errors := countResults(results)
	total := len(results)

	statusIcon := ":white_check_mark:"
	if errors > 0 {
		statusIcon = ":x:"
	} else if warnings > 0 {
		statusIcon = ":warning:"
	}

	headerText := fmt.Sprintf("%s *git-cascade compliance scan — %s*", statusIcon, org)
	totalRepos := len(uniqueRepos(results))
	summaryText := fmt.Sprintf("Scanned *%d* repositor%s — *%d* checks: *%d* passed, *%d* warnings, *%d* errors",
		totalRepos, map[bool]string{true: "y", false: "ies"}[totalRepos == 1],
		total, passes, warnings, errors)

	if listRepos {
		repos := uniqueRepos(results)
		if len(repos) > 0 {
			var repoList strings.Builder
			repoList.WriteString("\nrepositories:")
			for _, repo := range repos {
				repoList.WriteString("\n- ")
				repoList.WriteString(repo)
			}
			summaryText += repoList.String()
		}
	}

	if resultsURL != "" {
		summaryText += fmt.Sprintf("\n<%s|View compliance report>", resultsURL)
	}
	summaryText += fmt.Sprintf("\n_Scope: %s_", scopeSummary(scope))

	blocks := []slackBlock{
		{Type: "header", Text: &slackText{Type: "plain_text", Text: fmt.Sprintf("git-cascade: %s", org)}},
		{Type: "section", Text: &slackText{Type: "mrkdwn", Text: headerText + "\n" + summaryText}},
	}

	payload := slackPayload{
		Channel: channel,
		Text:    fmt.Sprintf("git-cascade scan complete for %s: %d errors, %d warnings", org, errors, warnings),
		Blocks:  blocks,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshalling slack payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("creating slack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("posting to slack: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack returned status %d", resp.StatusCode)
	}

	// The Slack Web API always returns 200 but signals errors in the JSON body.
	if token != "" {
		var apiResp slackAPIResponse
		if err := json.NewDecoder(resp.Body).Decode(&apiResp); err == nil && !apiResp.OK {
			return fmt.Errorf("slack API error: %s", apiResp.Error)
		}
	}

	return nil
}

// repoShortName returns the part after the last "/" in "owner/repo", or the
// original string if there is no "/".
func repoShortName(repo string) string {
	if i := strings.LastIndex(repo, "/"); i >= 0 {
		return repo[i+1:]
	}
	return repo
}

// splitTrimmed splits a comma-separated string and trims whitespace from each element.
func splitTrimmed(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func countResults(results []compliance.Result) (passes, warnings, errors int) {
	for _, r := range results {
		if r.Status != compliance.StatusFail {
			passes++
			continue
		}
		switch r.Severity {
		case config.SeverityError:
			errors++
		case config.SeverityWarning:
			warnings++
		default:
			passes++
		}
	}
	return
}

// uniqueRepos returns a sorted, deduplicated list of repo names from results.
func uniqueRepos(results []compliance.Result) []string {
	seen := make(map[string]struct{})
	var repos []string
	for _, r := range results {
		if _, ok := seen[r.Repo]; !ok {
			seen[r.Repo] = struct{}{}
			repos = append(repos, r.Repo)
		}
	}
	return repos
}

func groupByRepo(results []compliance.Result) map[string][]compliance.Result {
	m := make(map[string][]compliance.Result)
	for _, r := range results {
		m[r.Repo] = append(m[r.Repo], r)
	}
	return m
}
