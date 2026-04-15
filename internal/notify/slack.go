package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
)

// SlackPayload is a minimal Slack Incoming Webhook payload.
type slackPayload struct {
	Channel string        `json:"channel,omitempty"`
	Text    string        `json:"text"`
	Blocks  []slackBlock  `json:"blocks,omitempty"`
}

type slackBlock struct {
	Type string      `json:"type"`
	Text *slackText  `json:"text,omitempty"`
}

type slackText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// PostSlack sends a scan summary to a Slack Incoming Webhook.
// The webhook URL is resolved from cfg.WebhookURL, falling back to
// the GIT_CASCADE_SLACK_WEBHOOK environment variable.
// resultsURL is an optional runtime value (e.g. a GitHub Actions run URL) linked
// in the notification; supply an empty string to omit it.
func PostSlack(cfg config.SlackConfig, org string, results []compliance.Result, resultsURL string, scope config.Scope) error {
	webhookURL := cfg.WebhookURL
	if webhookURL == "" {
		webhookURL = os.Getenv("GIT_CASCADE_SLACK_WEBHOOK")
	}
	if webhookURL == "" {
		return fmt.Errorf("slack webhook URL not set (use notify.slack.webhook_url or GIT_CASCADE_SLACK_WEBHOOK)")
	}

	passes, warnings, errors := countResults(results)
	total := len(results)

	statusIcon := ":white_check_mark:"
	if errors > 0 {
		statusIcon = ":x:"
	} else if warnings > 0 {
		statusIcon = ":warning:"
	}

	totalRepos := countRepos(results)
	headerText := fmt.Sprintf("%s *git-cascade compliance scan — %s*", statusIcon, org)
	summaryText := fmt.Sprintf("Scanned *%d* repositor%s / *%d* checks: *%d* passed, *%d* warnings, *%d* errors",
		totalRepos, map[bool]string{true: "y", false: "ies"}[totalRepos == 1],
		total, passes, warnings, errors)

	if resultsURL == "" {
		resultsURL = os.Getenv("GIT_CASCADE_SLACK_RESULTS_URL")
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
		Channel: cfg.Channel,
		Text:    fmt.Sprintf("git-cascade scan complete for %s: %d errors, %d warnings", org, errors, warnings),
		Blocks:  blocks,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshalling slack payload: %w", err)
	}

	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(data)) //nolint:noctx
	if err != nil {
		return fmt.Errorf("posting to slack: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack webhook returned status %d", resp.StatusCode)
	}
	return nil
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

func groupByRepo(results []compliance.Result) map[string][]compliance.Result {
	m := make(map[string][]compliance.Result)
	for _, r := range results {
		m[r.Repo] = append(m[r.Repo], r)
	}
	return m
}
