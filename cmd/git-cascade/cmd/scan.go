package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/eukarya-inc/git-cascade/internal/notify"
	"github.com/eukarya-inc/git-cascade/internal/output"
	"github.com/spf13/cobra"
)

var scanFlags struct {
	// GitHub / auth
	org            string
	token          string
	appID          int64
	installationID int64
	privateKeyPath string

	// Config loading
	configRepo  string
	configPath  string
	configRef   string
	localConfig string

	// Repository filtering
	includeArchived bool
	skipPublic      bool
	skipPrivate     bool
	includeRepos    []string
	excludeRepos    []string

	// Output
	format     string
	outputPath string

	// Notifications
	slackWebhook   string
	slackChannel   string
	slackResultURL string
	issueMode      string
	issueRepo      string
	issueLabels    []string

	verbose bool
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

	// Config loading
	f.StringVar(&scanFlags.configRepo, "config-repo", compliance.DefaultConfigRepo, "Repository containing compliance configs")
	f.StringVar(&scanFlags.configPath, "config-path", compliance.DefaultConfigPath, "Path within the config repo to look for rule files")
	f.StringVar(&scanFlags.configRef, "config-ref", "", "Git ref for the config repo (default: repo default branch)")
	f.StringVar(&scanFlags.localConfig, "local-config", "", "Path to a local config directory (overrides remote config)")

	// Repository filtering
	f.BoolVar(&scanFlags.includeArchived, "include-archived", false, "Include archived repositories in scan")
	f.BoolVar(&scanFlags.skipPublic, "skip-public", false, "Skip public repositories")
	f.BoolVar(&scanFlags.skipPrivate, "skip-private", false, "Skip private repositories")
	f.StringSliceVar(&scanFlags.includeRepos, "include-repo", nil, "Only scan these repositories (repeatable)")
	f.StringSliceVar(&scanFlags.excludeRepos, "exclude-repo", nil, "Exclude these repositories from scan (repeatable)")

	// Output
	f.StringVar(&scanFlags.format, "format", "", "Output format: table, json, csv, sarif (default: table or config value)")
	f.StringVar(&scanFlags.outputPath, "output", "", "Write results to this file instead of stdout")

	// Slack
	f.StringVar(&scanFlags.slackWebhook, "slack-webhook", "", "Slack Incoming Webhook URL (or set GIT_CASCADE_SLACK_WEBHOOK)")
	f.StringVar(&scanFlags.slackChannel, "slack-channel", "", "Override Slack channel")
	f.StringVar(&scanFlags.slackResultURL, "slack-results-url", "", "URL to link in Slack notification")

	// GitHub Issues
	f.StringVar(&scanFlags.issueMode, "issue-mode", "", "Post findings as GitHub Issues: compliance or repo")
	f.StringVar(&scanFlags.issueRepo, "issue-repo", "", "owner/repo for consolidated issue (mode=compliance)")
	f.StringSliceVar(&scanFlags.issueLabels, "issue-label", nil, "Labels to apply to created issues (repeatable)")

	f.BoolVar(&scanFlags.verbose, "verbose", false, "Enable verbose logging")

	_ = scanCmd.MarkFlagRequired("org")
}

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan organization repositories for compliance",
	Long: `Scan all repositories in a GitHub organization against compliance rules.

Rules are loaded from the compliance repository in your organization by default,
or from a local directory if --local-config is specified.

By default, both public and private repositories are scanned. Use --skip-public
or --skip-private to disable scanning of either visibility. Use --include-repo to
restrict the scan to specific repositories, or --exclude-repo to skip certain ones.

Output can be written to a file with --output and formatted as table (default),
json, csv, or sarif (for GitHub Code Scanning).

After scanning, findings can be posted to Slack (--slack-webhook) or as GitHub
Issues (--issue-mode=compliance|repo).

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
  git-cascade scan --org myorg --issue-mode repo --issue-label compliance --issue-label automated`,
	RunE: runScan,
}

func runScan(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	logLevel := slog.LevelWarn
	if scanFlags.verbose {
		logLevel = slog.LevelDebug
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
	if cmd.Flags().Changed("include-repo") {
		filter.IncludeRepos = scanFlags.includeRepos
	}
	if cmd.Flags().Changed("exclude-repo") {
		filter.ExcludeRepos = scanFlags.excludeRepos
	}
	repos = filter.Apply(repos)
	logger.Info("repositories after filtering", "count", len(repos))

	// Run compliance checks
	engine := compliance.NewEngine(client, cfg, logger)
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

	// Slack notification
	slackCfg := cfg.Notify.Slack
	if cmd.Flags().Changed("slack-webhook") {
		slackCfg.Enabled = true
		slackCfg.WebhookURL = scanFlags.slackWebhook
	}
	if cmd.Flags().Changed("slack-channel") {
		slackCfg.Channel = scanFlags.slackChannel
	}
	if slackCfg.Enabled || slackCfg.WebhookURL != "" {
		logger.Info("sending slack notification")
		if err := notify.PostSlack(slackCfg, scanFlags.org, results, scanFlags.slackResultURL); err != nil {
			return fmt.Errorf("slack notification: %w", err)
		}
	}

	// GitHub Issues
	issueCfg := cfg.Notify.Issues
	if cmd.Flags().Changed("issue-mode") {
		issueCfg.Enabled = true
		issueCfg.Mode = scanFlags.issueMode
	}
	if cmd.Flags().Changed("issue-repo") {
		issueCfg.ComplianceRepo = scanFlags.issueRepo
	}
	if cmd.Flags().Changed("issue-label") {
		issueCfg.Labels = scanFlags.issueLabels
	}
	if issueCfg.Enabled {
		logger.Info("posting GitHub Issues", "mode", issueCfg.Mode)
		if err := notify.PostIssues(ctx, client, issueCfg, scanFlags.org, results); err != nil {
			return fmt.Errorf("posting issues: %w", err)
		}
	}

	if output.HasFailures(results) {
		return fmt.Errorf("compliance check failed: one or more rules with error severity did not pass")
	}

	return nil
}

func resolveCredentials() (gh.Credentials, error) {
	if scanFlags.token != "" {
		return gh.Credentials{
			Method: gh.AuthPAT,
			Token:  scanFlags.token,
		}, nil
	}
	if scanFlags.appID != 0 {
		return gh.Credentials{
			Method:         gh.AuthGitHubApp,
			AppID:          scanFlags.appID,
			InstallationID: scanFlags.installationID,
			PrivateKeyPath: scanFlags.privateKeyPath,
		}, nil
	}
	return gh.CredentialsFromEnv()
}
