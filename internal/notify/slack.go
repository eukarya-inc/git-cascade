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
func PostSlack(cfg config.SlackConfig, org string, results []compliance.Result) error {
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

	headerText := fmt.Sprintf("%s *git-cascade compliance scan — %s*", statusIcon, org)
	summaryText := fmt.Sprintf("Scanned *%d* checks: *%d* passed, *%d* warnings, *%d* errors",
		total, passes, warnings, errors)

	if cfg.ResultsURL != "" {
		summaryText += fmt.Sprintf("\n<%s|View full results>", cfg.ResultsURL)
	}

	// Group failures by repo
	var failLines []string
	byRepo := groupByRepo(results)
	for repo, repoResults := range byRepo {
		var repoFails []string
		for _, r := range repoResults {
			if r.Status == compliance.StatusFail {
				repoFails = append(repoFails, fmt.Sprintf("  • `%s` (%s): %s", r.RuleID, r.Severity, r.Message))
			}
		}
		if len(repoFails) > 0 {
			failLines = append(failLines, fmt.Sprintf("*%s*", repo))
			failLines = append(failLines, repoFails...)
		}
	}

	blocks := []slackBlock{
		{Type: "header", Text: &slackText{Type: "plain_text", Text: fmt.Sprintf("git-cascade: %s", org)}},
		{Type: "section", Text: &slackText{Type: "mrkdwn", Text: headerText + "\n" + summaryText}},
	}

	if len(failLines) > 0 {
		blocks = append(blocks, slackBlock{Type: "divider"})
		// Slack block text is capped at 3000 chars; truncate if needed
		body := strings.Join(failLines, "\n")
		if len(body) > 2900 {
			body = body[:2900] + "\n…(truncated)"
		}
		blocks = append(blocks, slackBlock{
			Type: "section",
			Text: &slackText{Type: "mrkdwn", Text: body},
		})
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
