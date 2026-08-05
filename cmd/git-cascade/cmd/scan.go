package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/eukarya-inc/git-cascade/internal/notify"
	"github.com/eukarya-inc/git-cascade/internal/output"
	"github.com/eukarya-inc/git-cascade/internal/remediation"
	_ "github.com/eukarya-inc/git-cascade/internal/remediation/fixes"
	"github.com/spf13/cobra"
)

var scanFlags struct {
	// GitHub / auth
	org            string
	token          string
	appID          int64
	installationID int64
	privateKeyPath string

	// GitHub / auth for notifications (GitHub Issues), when the notify
	// target (e.g. a compliance repo in a different org) needs different
	// credentials than the org being scanned. Falls back to the scan
	// credentials above when left unset.
	notifyToken          string
	notifyAppID          int64
	notifyInstallationID int64
	notifyPrivateKeyPath string

	// GitHub / auth for auto-remediation (opening fix PRs). Unlike notify
	// credentials, this set has no fallback to the scan credentials — it
	// must be supplied explicitly whenever remediation.enabled is true,
	// since it writes directly to scanned repositories.
	remediateToken          string
	remediateAppID          int64
	remediateInstallationID int64
	remediatePrivateKeyPath string

	// Config loading
	configRepo  string
	configPath  string
	configRef   string
	localConfig string

	// Repository filtering
	includeArchived bool
	includeForked   bool
	skipPublic      bool
	skipPrivate     bool
	includeRepos    []string
	excludeRepos    []string

	// Output
	format     string
	outputPath string

	// Notifications
	slackWebhook    string
	slackBotToken   string
	slackChannel    string
	slackResultURL  string
	issueMode       string
	issueRepo       string
	issueHeader     string
	issueTitle      string
	issueSectionKey string
	issueLabels     []string

	concurrency int
	silent      bool
}

func init() {
	rootCmd.AddCommand(scanCmd)

	f := scanCmd.Flags()

	// GitHub / auth
	f.StringVar(&scanFlags.org, "org", "", "GitHub organization to scan (required)")
	f.StringVar(&scanFlags.token, "token", "", "GitHub Personal Access Token (or set GIT_CASCADE_TOKEN)")
	f.Int64Var(&scanFlags.appID, "app-id", 0, "GitHub App ID")
	f.Int64Var(&scanFlags.installationID, "installation-id", 0, "GitHub App Installation ID")
	f.StringVar(&scanFlags.privateKeyPath, "private-key-path", "", "Path to GitHub App private key PEM file")

	// GitHub / auth for notifications (defaults to scan credentials above)
	f.StringVar(&scanFlags.notifyToken, "notify-token", "", "GitHub Personal Access Token for posting notifications, if different from --token (or set GIT_CASCADE_NOTIFY_TOKEN)")
	f.Int64Var(&scanFlags.notifyAppID, "notify-app-id", 0, "GitHub App ID for posting notifications, if different from --app-id")
	f.Int64Var(&scanFlags.notifyInstallationID, "notify-installation-id", 0, "GitHub App Installation ID for posting notifications, if different from --installation-id")
	f.StringVar(&scanFlags.notifyPrivateKeyPath, "notify-private-key-path", "", "Path to GitHub App private key PEM file for posting notifications, if different from --private-key-path")

	// GitHub / auth for auto-remediation (required whenever remediation.enabled is true; no fallback)
	f.StringVar(&scanFlags.remediateToken, "remediate-token", "", "GitHub Personal Access Token for opening auto-remediation pull requests (or set GIT_CASCADE_REMEDIATE_TOKEN)")
	f.Int64Var(&scanFlags.remediateAppID, "remediate-app-id", 0, "GitHub App ID for opening auto-remediation pull requests")
	f.Int64Var(&scanFlags.remediateInstallationID, "remediate-installation-id", 0, "GitHub App Installation ID for opening auto-remediation pull requests")
	f.StringVar(&scanFlags.remediatePrivateKeyPath, "remediate-private-key-path", "", "Path to GitHub App private key PEM file for opening auto-remediation pull requests")

	// Config loading
	f.StringVar(&scanFlags.configRepo, "config-repo", compliance.DefaultConfigRepo, "Repository containing compliance configs")
	f.StringVar(&scanFlags.configPath, "config-path", compliance.DefaultConfigPath, "Path within the config repo to look for rule files")
	f.StringVar(&scanFlags.configRef, "config-ref", "", "Git ref for the config repo (default: repo default branch)")
	f.StringVar(&scanFlags.localConfig, "local-config", "", "Path to a local config directory (overrides remote config)")

	// Repository filtering
	f.BoolVar(&scanFlags.includeArchived, "include-archived", false, "Include archived repositories in scan")
	f.BoolVar(&scanFlags.includeForked, "include-forked", false, "Include forked repositories in scan")
	f.BoolVar(&scanFlags.skipPublic, "skip-public", false, "Skip public repositories")
	f.BoolVar(&scanFlags.skipPrivate, "skip-private", false, "Skip private repositories")
	f.StringSliceVar(&scanFlags.includeRepos, "include-repo", nil, "Only scan these repositories (repeatable)")
	f.StringSliceVar(&scanFlags.excludeRepos, "exclude-repo", nil, "Exclude these repositories from scan (repeatable)")

	// Output
	f.StringVar(&scanFlags.format, "format", "", "Output format: table, json, csv, sarif (default: table or config value)")
	f.StringVar(&scanFlags.outputPath, "output", "", "Write results to this file instead of stdout")

	// Slack
	f.StringVar(&scanFlags.slackWebhook, "slack-webhook", "", "Slack Incoming Webhook URL (or set GIT_CASCADE_SLACK_WEBHOOK)")
	f.StringVar(&scanFlags.slackBotToken, "slack-bot-token", "", "Slack bot OAuth token for per-channel routing (or set GIT_CASCADE_SLACK_BOT_TOKEN)")
	f.StringVar(&scanFlags.slackChannel, "slack-channel", "", "Override Slack channel")
	f.StringVar(&scanFlags.slackResultURL, "slack-results-url", "", "URL to link in Slack notification")

	// GitHub Issues
	f.StringVar(&scanFlags.issueMode, "issue-mode", "", "Post findings as GitHub Issues: compliance, repo, or append")
	f.StringVar(&scanFlags.issueRepo, "issue-repo", "", "owner/repo for consolidated or shared issue (mode=compliance|append)")
	f.StringVar(&scanFlags.issueHeader, "issue-header", "", "Override the issue body heading (mode=compliance, default: \"# Compliance Report — {org}\")")
	f.StringVar(&scanFlags.issueTitle, "issue-title", "", "Issue title (required for mode=append; overrides the default title for mode=compliance|repo)")
	f.StringVar(&scanFlags.issueSectionKey, "issue-section-key", "", "Identifies this config's comment on a shared issue (mode=append, default: org)")
	f.StringSliceVar(&scanFlags.issueLabels, "issue-label", nil, "Labels to apply to created issues (repeatable)")

	f.IntVar(&scanFlags.concurrency, "concurrency", 0, "Number of concurrent checks (default 5, or GIT_CASCADE_CONCURRENCY)")
	f.BoolVar(&scanFlags.silent, "silent", false, "Suppress progress logging")

	_ = scanCmd.MarkFlagRequired("org")
}

var scanCmd = &cobra.Command{
	Use:          "scan",
	Short:        "Scan organization repositories for compliance",
	SilenceUsage: true,
	Long: `Scan all repositories in a GitHub organization against compliance rules.

Rules are loaded from the compliance repository in your organization by default,
or from a local directory if --local-config is specified.

By default, both public and private repositories are scanned. Use --skip-public
or --skip-private to disable scanning of either visibility. Use --include-repo to
restrict the scan to specific repositories, or --exclude-repo to skip certain ones.

Output can be written to a file with --output and formatted as table (default),
json, csv, or sarif (for GitHub Code Scanning).

After scanning, findings can be posted to Slack (--slack-webhook) or as GitHub
Issues (--issue-mode=compliance|repo|append).

Examples:
  # Scan using PAT from environment
  git-cascade scan --org myorg

  # Write SARIF output for GitHub Code Scanning
  git-cascade scan --org myorg --format sarif --output results.sarif

  # Write CSV to a file
  git-cascade scan --org myorg --format csv --output findings.csv

  # Notify Slack
  git-cascade scan --org myorg --slack-webhook https://hooks.slack.com/...

  # Post consolidated GitHub Issue in the compliance repo
  git-cascade scan --org myorg --issue-mode compliance

  # Post one issue per failing repo
  git-cascade scan --org myorg --issue-mode repo --issue-label compliance --issue-label automated

  # Append findings as a comment on a shared, integrated issue used by other scanning tools
  git-cascade scan --org myorg --issue-mode append --issue-repo myorg/security --issue-title "Integrated Findings"

  # Same shared issue, but from a second config that must not overwrite the first's comment
  git-cascade scan --org myorg --issue-mode append --issue-repo myorg/security --issue-title "Integrated Findings" --issue-section-key myorg-frontend

  # Scan org-a but post the compliance issue into org-b, using a separate token
  # scoped to org-b (falls back to --token when --notify-token is unset)
  git-cascade scan --org org-a --token $ORG_A_TOKEN --issue-mode compliance --issue-repo org-b/compliance --notify-token $ORG_B_TOKEN`,
	RunE: runScan,
}

func runScan(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	logLevel := slog.LevelInfo
	if scanFlags.silent {
		logLevel = slog.LevelWarn
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))

	// Resolve credentials
	creds, err := resolveCredentials()
	if err != nil {
		return err
	}

	client, err := gh.NewClient(creds)
	if err != nil {
		return fmt.Errorf("creating GitHub client: %w", err)
	}

	// Load compliance config
	var cfg *config.ComplianceConfig
	if scanFlags.localConfig != "" {
		logger.Info("loading config from local directory", "path", scanFlags.localConfig)
		cfg, err = config.LoadAll(scanFlags.localConfig)
	} else {
		logger.Info("loading config from repository", "repo", fmt.Sprintf("%s/%s", scanFlags.org, scanFlags.configRepo))
		cfg, err = compliance.LoadConfigFromRepo(ctx, client, scanFlags.org, scanFlags.configRepo, scanFlags.configPath, scanFlags.configRef)
	}
	if err != nil {
		return fmt.Errorf("loading compliance config: %w", err)
	}
	logger.Info("loaded rules", "count", len(cfg.Rules))

	// List repos
	logger.Info("listing repositories", "org", scanFlags.org)
	repos, err := gh.ListOrgRepos(ctx, client, scanFlags.org)
	if err != nil {
		return err
	}
	logger.Info("fetched repositories", "count", len(repos))

	// Build filter: start from YAML scope, then apply CLI overrides
	filter := gh.RepoFilterFromScope(cfg.Scope)
	if cmd.Flags().Changed("skip-public") {
		filter.IncludePublic = !scanFlags.skipPublic
	}
	if cmd.Flags().Changed("skip-private") {
		filter.IncludePrivate = !scanFlags.skipPrivate
	}
	if cmd.Flags().Changed("include-archived") {
		filter.IncludeArchived = scanFlags.includeArchived
	}
	if cmd.Flags().Changed("include-forked") {
		filter.IncludeForked = scanFlags.includeForked
	}
	if cmd.Flags().Changed("include-repo") {
		filter.IncludeRepos = scanFlags.includeRepos
	}
	if cmd.Flags().Changed("exclude-repo") {
		filter.ExcludeRepos = scanFlags.excludeRepos
	}
	repos = filter.Apply(repos)
	logger.Info("repositories after filtering", "count", len(repos))

	// Run compliance checks
	concurrency := scanFlags.concurrency
	if concurrency <= 0 {
		if v := os.Getenv("GIT_CASCADE_CONCURRENCY"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				concurrency = n
			}
		}
	}
	engine := compliance.NewEngine(client, cfg, logger).WithConcurrency(concurrency)
	results, err := engine.Run(ctx, repos)
	if err != nil {
		return err
	}

	// Resolve output options: CLI flags override config values
	outOpts := output.Options{
		Format:     output.Format(cfg.Output.Format),
		OutputPath: cfg.Output.Path,
	}
	if cmd.Flags().Changed("format") {
		outOpts.Format = output.Format(scanFlags.format)
	}
	if cmd.Flags().Changed("output") {
		outOpts.OutputPath = scanFlags.outputPath
	}
	if outOpts.Format == "" {
		outOpts.Format = output.FormatTable
	}

	if err := output.Write(os.Stdout, results, outOpts); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	// Auto-remediation — opens/updates fix PRs for failing rules that have
	// auto_remediation enabled (per-rule or via the remediation config
	// default) and a registered remediator. Requires its own credentials;
	// scan/notify tokens are never silently reused for repo writes.
	if cfg.Remediation.Enabled {
		remediateCreds, err := resolveRemediateCredentials()
		if err != nil {
			return fmt.Errorf("resolving remediate credentials: %w", err)
		}
		remediateClient, err := gh.NewClient(remediateCreds)
		if err != nil {
			return fmt.Errorf("creating remediate GitHub client: %w", err)
		}

		rulesByID := make(map[string]config.Rule, len(cfg.Rules))
		for _, r := range cfg.Rules {
			rulesByID[r.ID] = r
		}
		reposByName := make(map[string]gh.Repository, len(repos))
		for _, r := range repos {
			reposByName[r.FullName] = r
		}

		logger.Info("running auto-remediation")
		outcomes := remediation.Run(ctx, remediateClient, cfg.Remediation, results, rulesByID, reposByName, logger)
		for _, o := range outcomes {
			if o.Err != nil {
				logger.Warn("remediation failed", "rule", o.RuleID, "repo", o.Repo, "error", o.Err)
			} else if o.PRURL != "" {
				logger.Info("remediation PR", "rule", o.RuleID, "repo", o.Repo, "url", o.PRURL)
			}
		}
	}

	// GitHub Issues — CLI flags > env vars > config file
	// Run before Slack so the issue URL can be linked in the notification.
	issueCfg := cfg.Notify.Issues
	if cmd.Flags().Changed("issue-mode") {
		issueCfg.Enabled = true
		issueCfg.Mode = scanFlags.issueMode
	} else if v := os.Getenv("GIT_CASCADE_ISSUE_MODE"); v != "" && issueCfg.Mode == "" {
		issueCfg.Enabled = true
		issueCfg.Mode = v
	}
	if cmd.Flags().Changed("issue-repo") {
		issueCfg.ComplianceRepo = scanFlags.issueRepo
	} else if v := os.Getenv("GIT_CASCADE_ISSUE_REPO"); v != "" && issueCfg.ComplianceRepo == "" {
		issueCfg.ComplianceRepo = v
	}
	if cmd.Flags().Changed("issue-header") {
		issueCfg.HeaderText = scanFlags.issueHeader
	} else if v := os.Getenv("GIT_CASCADE_ISSUE_HEADER"); v != "" && issueCfg.HeaderText == "" {
		issueCfg.HeaderText = v
	}
	if cmd.Flags().Changed("issue-title") {
		issueCfg.IssueTitle = scanFlags.issueTitle
	} else if v := os.Getenv("GIT_CASCADE_ISSUE_TITLE"); v != "" && issueCfg.IssueTitle == "" {
		issueCfg.IssueTitle = v
	}
	if cmd.Flags().Changed("issue-section-key") {
		issueCfg.SectionKey = scanFlags.issueSectionKey
	} else if v := os.Getenv("GIT_CASCADE_ISSUE_SECTION_KEY"); v != "" && issueCfg.SectionKey == "" {
		issueCfg.SectionKey = v
	}
	if cmd.Flags().Changed("issue-label") {
		issueCfg.Labels = scanFlags.issueLabels
	}
	ciURL := scanFlags.slackResultURL
	if ciURL == "" {
		ciURL = os.Getenv("GIT_CASCADE_SLACK_RESULTS_URL")
	}
	var issueURL string
	if issueCfg.Enabled {
		notifyCreds, err := resolveNotifyCredentials(creds)
		if err != nil {
			return fmt.Errorf("resolving notify credentials: %w", err)
		}
		notifyClient, err := gh.NewClient(notifyCreds)
		if err != nil {
			return fmt.Errorf("creating notify GitHub client: %w", err)
		}
		logger.Info("posting GitHub Issues", "mode", issueCfg.Mode)
		issueURL, err = notify.PostIssues(ctx, notifyClient, issueCfg, scanFlags.org, results, ciURL, cfg.Scope)
		if err != nil {
			return fmt.Errorf("posting issues: %w", err)
		}
	}

	// Slack notification — CLI flags > env vars > config file
	slackCfg := cfg.Notify.Slack
	if cmd.Flags().Changed("slack-webhook") {
		slackCfg.Enabled = true
		slackCfg.WebhookURL = scanFlags.slackWebhook
	} else if v := os.Getenv("GIT_CASCADE_SLACK_WEBHOOK"); v != "" && slackCfg.WebhookURL == "" {
		slackCfg.WebhookURL = v
	}
	if cmd.Flags().Changed("slack-bot-token") {
		slackCfg.Enabled = true
		slackCfg.BotToken = scanFlags.slackBotToken
	} else if v := os.Getenv("GIT_CASCADE_SLACK_BOT_TOKEN"); v != "" && slackCfg.BotToken == "" {
		slackCfg.BotToken = v
	}
	if cmd.Flags().Changed("slack-channel") {
		slackCfg.Channel = scanFlags.slackChannel
	} else if v := os.Getenv("GIT_CASCADE_SLACK_CHANNEL"); v != "" && slackCfg.Channel == "" {
		slackCfg.Channel = v
	}
	// If an issue URL is available, use it as the results URL (links to the issue).
	// Otherwise fall back to the CI run URL.
	resultsURL := issueURL
	if resultsURL == "" {
		resultsURL = ciURL
	}
	if slackCfg.Enabled || slackCfg.WebhookURL != "" || slackCfg.BotToken != "" {
		logger.Info("sending slack notification")
		if err := notify.PostSlack(slackCfg, scanFlags.org, results, resultsURL); err != nil {
			return fmt.Errorf("slack notification: %w", err)
		}
	}

	if output.HasFailures(results) {
		return fmt.Errorf("compliance check failed: one or more rules with error severity did not pass")
	}

	return nil
}

func resolveCredentials() (gh.Credentials, error) {
	return resolveCredentialsFrom(credentialFlags{
		token:          scanFlags.token,
		appID:          scanFlags.appID,
		installationID: scanFlags.installationID,
		privateKeyPath: scanFlags.privateKeyPath,
		tokenEnv:       "GIT_CASCADE_TOKEN",
		appIDEnv:       "GIT_CASCADE_APP_ID",
		installIDEnv:   "GIT_CASCADE_INSTALLATION_ID",
		privateKeyEnv:  "GIT_CASCADE_PRIVATE_KEY_PATH",
		flagPrefix:     "--",
	})
}

// resolveRemediateCredentials resolves credentials for opening auto-remediation
// pull requests. Unlike resolveNotifyCredentials, this never falls back to the
// scan credentials: remediation writes directly to scanned repositories, so an
// explicit --remediate-* flag or GIT_CASCADE_REMEDIATE_* env var is required.
func resolveRemediateCredentials() (gh.Credentials, error) {
	if scanFlags.remediateToken == "" && scanFlags.remediateAppID == 0 &&
		scanFlags.remediateInstallationID == 0 && scanFlags.remediatePrivateKeyPath == "" &&
		os.Getenv("GIT_CASCADE_REMEDIATE_TOKEN") == "" && os.Getenv("GIT_CASCADE_REMEDIATE_APP_ID") == "" &&
		os.Getenv("GIT_CASCADE_REMEDIATE_INSTALLATION_ID") == "" && os.Getenv("GIT_CASCADE_REMEDIATE_PRIVATE_KEY_PATH") == "" {
		return gh.Credentials{}, fmt.Errorf("remediation.enabled is true but no --remediate-token / --remediate-app-id credentials (or GIT_CASCADE_REMEDIATE_* env vars) were supplied")
	}
	return resolveCredentialsFrom(credentialFlags{
		token:          scanFlags.remediateToken,
		appID:          scanFlags.remediateAppID,
		installationID: scanFlags.remediateInstallationID,
		privateKeyPath: scanFlags.remediatePrivateKeyPath,
		tokenEnv:       "GIT_CASCADE_REMEDIATE_TOKEN",
		appIDEnv:       "GIT_CASCADE_REMEDIATE_APP_ID",
		installIDEnv:   "GIT_CASCADE_REMEDIATE_INSTALLATION_ID",
		privateKeyEnv:  "GIT_CASCADE_REMEDIATE_PRIVATE_KEY_PATH",
		flagPrefix:     "--remediate-",
	})
}

// resolveNotifyCredentials resolves credentials for posting notifications
// (e.g. GitHub Issues). It falls back to scanCreds when no --notify-* flag
// or GIT_CASCADE_NOTIFY_* env var is set, so a single-org setup needs no
// extra configuration; a cross-org compliance repo can supply its own token
// or GitHub App identity via --notify-token / --notify-app-id etc.
func resolveNotifyCredentials(scanCreds gh.Credentials) (gh.Credentials, error) {
	if scanFlags.notifyToken == "" && scanFlags.notifyAppID == 0 &&
		scanFlags.notifyInstallationID == 0 && scanFlags.notifyPrivateKeyPath == "" &&
		os.Getenv("GIT_CASCADE_NOTIFY_TOKEN") == "" && os.Getenv("GIT_CASCADE_NOTIFY_APP_ID") == "" &&
		os.Getenv("GIT_CASCADE_NOTIFY_INSTALLATION_ID") == "" && os.Getenv("GIT_CASCADE_NOTIFY_PRIVATE_KEY_PATH") == "" {
		return scanCreds, nil
	}
	return resolveCredentialsFrom(credentialFlags{
		token:          scanFlags.notifyToken,
		appID:          scanFlags.notifyAppID,
		installationID: scanFlags.notifyInstallationID,
		privateKeyPath: scanFlags.notifyPrivateKeyPath,
		tokenEnv:       "GIT_CASCADE_NOTIFY_TOKEN",
		appIDEnv:       "GIT_CASCADE_NOTIFY_APP_ID",
		installIDEnv:   "GIT_CASCADE_NOTIFY_INSTALLATION_ID",
		privateKeyEnv:  "GIT_CASCADE_NOTIFY_PRIVATE_KEY_PATH",
		flagPrefix:     "--notify-",
	})
}

// credentialFlags names the CLI flag values and env var keys resolveCredentialsFrom
// should read for one credential set (scan or notify).
type credentialFlags struct {
	token          string
	appID          int64
	installationID int64
	privateKeyPath string
	tokenEnv       string
	appIDEnv       string
	installIDEnv   string
	privateKeyEnv  string
	flagPrefix     string
}

func resolveCredentialsFrom(f credentialFlags) (gh.Credentials, error) {
	// PAT: flag takes priority, then env var
	if f.token != "" {
		return gh.Credentials{Method: gh.AuthPAT, Token: f.token}, nil
	}
	if v := os.Getenv(f.tokenEnv); v != "" {
		return gh.Credentials{Method: gh.AuthPAT, Token: v}, nil
	}

	// GitHub App: merge CLI flags with env var fallbacks so partial flag sets work
	appID := f.appID
	installationID := f.installationID
	privateKeyPath := f.privateKeyPath
	if appID == 0 {
		if v := os.Getenv(f.appIDEnv); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				appID = n
			}
		}
	}
	if installationID == 0 {
		if v := os.Getenv(f.installIDEnv); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				installationID = n
			}
		}
	}
	if privateKeyPath == "" {
		privateKeyPath = os.Getenv(f.privateKeyEnv)
	}
	if appID != 0 || installationID != 0 || privateKeyPath != "" {
		// At least one App field was set — validate all three are present
		if appID == 0 || installationID == 0 || privateKeyPath == "" {
			return gh.Credentials{}, fmt.Errorf(
				"GitHub App auth requires %sapp-id, %sinstallation-id, and %sprivate-key-path (or their env var equivalents)",
				f.flagPrefix, f.flagPrefix, f.flagPrefix,
			)
		}
		return gh.Credentials{
			Method:         gh.AuthGitHubApp,
			AppID:          appID,
			InstallationID: installationID,
			PrivateKeyPath: privateKeyPath,
		}, nil
	}

	// Fall back to env-only resolution (PAT via GIT_CASCADE_TOKEN / GITHUB_TOKEN)
	return gh.CredentialsFromEnv()
}
