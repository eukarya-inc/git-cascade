package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ComplianceConfig is the top-level configuration loaded from YAML.
type ComplianceConfig struct {
	Version  string   `yaml:"version"`
	Scope    Scope    `yaml:"scope,omitempty"`
	Output   Output   `yaml:"output,omitempty"`
	Notify   Notify   `yaml:"notify,omitempty"`
	Rules    []Rule   `yaml:"rules"`
}

// Output configures default output behaviour.
type Output struct {
	// Format is the default output format: table, json, csv, or sarif.
	Format string `yaml:"format,omitempty"`
	// Path is the default file path to write results to. Empty means stdout.
	Path string `yaml:"path,omitempty"`
}

// Notify configures where scan results are posted after a run.
type Notify struct {
	Slack  SlackConfig  `yaml:"slack,omitempty"`
	Issues IssuesConfig `yaml:"issues,omitempty"`
}

// SlackConfig configures Slack notifications.
type SlackConfig struct {
	// Enabled controls whether Slack notifications are sent.
	Enabled bool `yaml:"enabled,omitempty"`
	// WebhookURL is the Incoming Webhook URL. Prefer the GIT_CASCADE_SLACK_WEBHOOK env var.
	// Use this for simple single-channel delivery.
	WebhookURL string `yaml:"webhook_url,omitempty"`
	// BotToken is a Slack bot user OAuth token (xoxb-...).
	// Prefer the GIT_CASCADE_SLACK_BOT_TOKEN env var.
	// Required when using RepositoryChannels for per-channel routing.
	BotToken string `yaml:"bot_token,omitempty"`
	// Channel is the default fallback channel (name or ID).
	// Used as the webhook channel override, or as the bot fallback for unmapped repos.
	Channel string `yaml:"channel,omitempty"`
	// RepositoryChannels maps specific repositories to one or more channels.
	// Results for a repository are sent to every channel it is mapped to.
	// Repositories not listed here fall back to the default Channel (if set).
	// ResultsURL is not stored in config — it is always a runtime value supplied
	// via --slack-results-url flag or GIT_CASCADE_SLACK_RESULTS_URL env var.
	RepositoryChannels []RepositoryChannelMapping `yaml:"repository_channels,omitempty"`
}

// RepositoryChannelMapping routes results for a set of repositories to one or
// more Slack channels.  Both Channels and Repositories are comma-separated
// strings so they are easy to write inline in YAML.
type RepositoryChannelMapping struct {
	// Channels is a comma-separated list of Slack channel names (e.g. "#ops, #security").
	Channels string `yaml:"channels"`
	// Repositories is a comma-separated list of repository names as they appear
	// in scan results (short name or owner/repo, must match consistently).
	Repositories string `yaml:"repositories"`
}

// IssuesConfig configures GitHub Issues posting.
type IssuesConfig struct {
	// Enabled controls whether issues are created/updated after a scan.
	Enabled bool `yaml:"enabled,omitempty"`
	// Mode controls where findings are posted:
	//   "compliance" – one consolidated issue in the compliance config repo
	//   "repo"       – one issue per scanned repository that has findings
	Mode string `yaml:"mode,omitempty"`
	// ComplianceRepo is the owner/repo for consolidated issue posting (mode=compliance).
	// Defaults to the org's compliance repository used for config loading.
	ComplianceRepo string `yaml:"compliance_repo,omitempty"`
	// Labels are applied to every created/updated issue.
	Labels []string `yaml:"labels,omitempty"`
}

// Scope defines which repositories are targeted by the compliance scan.
// When omitted, defaults to scanning all public and private repos, excluding archived and forked.
type Scope struct {
	IncludePublic   *bool    `yaml:"include_public,omitempty"`
	IncludePrivate  *bool    `yaml:"include_private,omitempty"`
	IncludeArchived *bool    `yaml:"include_archived,omitempty"`
	IncludeForked   *bool    `yaml:"include_forked,omitempty"`
	IncludeRepos    []string `yaml:"include_repos,omitempty"`
	ExcludeRepos    []string `yaml:"exclude_repos,omitempty"`
}

// Rule defines a single compliance check.
type Rule struct {
	ID          string              `yaml:"id"`
	Name        string              `yaml:"name"`
	Description string              `yaml:"description"`
	Severity    Severity            `yaml:"severity"`
	Enabled     bool                `yaml:"enabled"`
	Params      map[string]string   `yaml:"params,omitempty"`
	ListParams  map[string][]string `yaml:"-"`
}

// UnmarshalYAML implements yaml.Unmarshaler so that the params block can hold
// both scalar string values (key: "value") and sequence values (key: ["a","b"]).
// Scalar values are stored in Params; sequence values are stored in ListParams.
func (r *Rule) UnmarshalYAML(value *yaml.Node) error {
	// Use an alias type to avoid infinite recursion when calling Decode.
	type ruleAlias struct {
		ID          string   `yaml:"id"`
		Name        string   `yaml:"name"`
		Description string   `yaml:"description"`
		Severity    Severity `yaml:"severity"`
		Enabled     bool     `yaml:"enabled"`
	}
	var alias ruleAlias
	if err := value.Decode(&alias); err != nil {
		return err
	}
	r.ID = alias.ID
	r.Name = alias.Name
	r.Description = alias.Description
	r.Severity = alias.Severity
	r.Enabled = alias.Enabled

	// Find the params mapping node.
	for i := 0; i+1 < len(value.Content); i += 2 {
		keyNode := value.Content[i]
		valNode := value.Content[i+1]
		if keyNode.Value != "params" {
			continue
		}
		if valNode.Kind != yaml.MappingNode {
			break
		}
		for j := 0; j+1 < len(valNode.Content); j += 2 {
			k := valNode.Content[j].Value
			v := valNode.Content[j+1]
			switch v.Kind {
			case yaml.SequenceNode:
				if r.ListParams == nil {
					r.ListParams = make(map[string][]string)
				}
				var items []string
				for _, item := range v.Content {
					items = append(items, item.Value)
				}
				r.ListParams[k] = items
			default:
				if r.Params == nil {
					r.Params = make(map[string]string)
				}
				r.Params[k] = v.Value
			}
		}
	}
	return nil
}

// Severity represents how critical a compliance violation is.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Load reads a compliance config from a YAML file.
func Load(path string) (*ComplianceConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %s: %w", path, err)
	}
	return Parse(data)
}

// Parse parses YAML bytes into a ComplianceConfig and validates it.
func Parse(data []byte) (*ComplianceConfig, error) {
	var cfg ComplianceConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ParsePartial parses YAML bytes without validation, for use when merging
// multiple files where version may only be present in one of them.
func ParsePartial(data []byte) (*ComplianceConfig, error) {
	var cfg ComplianceConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
}

// Validate checks that the config is well-formed.
func (c *ComplianceConfig) Validate() error {
	if c.Version == "" {
		return fmt.Errorf("config: version is required")
	}
	seen := make(map[string]bool)
	for i, r := range c.Rules {
		if r.ID == "" {
			return fmt.Errorf("config: rule at index %d is missing an id", i)
		}
		if seen[r.ID] {
			return fmt.Errorf("config: duplicate rule id %q", r.ID)
		}
		seen[r.ID] = true
		switch r.Severity {
		case SeverityError, SeverityWarning, SeverityInfo:
		default:
			return fmt.Errorf("config: rule %q has invalid severity %q (must be error, warning, or info)", r.ID, r.Severity)
		}
	}
	return nil
}

// LoadAll loads all YAML files from a directory and merges them into a single config.
func LoadAll(dir string) (*ComplianceConfig, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading config directory %s: %w", dir, err)
	}

	merged := &ComplianceConfig{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yml" && ext != ".yaml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", entry.Name(), err)
		}
		cfg, err := ParsePartial(data)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", entry.Name(), err)
		}
		if merged.Version == "" {
			merged.Version = cfg.Version
		}
		merged.Scope = merged.Scope.Merge(cfg.Scope)
		if merged.Output.Format == "" {
			merged.Output.Format = cfg.Output.Format
		}
		if merged.Output.Path == "" {
			merged.Output.Path = cfg.Output.Path
		}
		if !merged.Notify.Slack.Enabled {
			merged.Notify.Slack = cfg.Notify.Slack
		}
		if !merged.Notify.Issues.Enabled {
			merged.Notify.Issues = cfg.Notify.Issues
		}
		merged.Rules = append(merged.Rules, cfg.Rules...)
	}

	if err := merged.Validate(); err != nil {
		return nil, err
	}
	return merged, nil
}

// Merge combines two Scopes. Values from other override only if they are explicitly set (non-nil).
// Slice fields (IncludeRepos, ExcludeRepos) are appended.
func (s Scope) Merge(other Scope) Scope {
	out := s
	if other.IncludePublic != nil {
		out.IncludePublic = other.IncludePublic
	}
	if other.IncludePrivate != nil {
		out.IncludePrivate = other.IncludePrivate
	}
	if other.IncludeArchived != nil {
		out.IncludeArchived = other.IncludeArchived
	}
	if other.IncludeForked != nil {
		out.IncludeForked = other.IncludeForked
	}
	out.IncludeRepos = append(out.IncludeRepos, other.IncludeRepos...)
	out.ExcludeRepos = append(out.ExcludeRepos, other.ExcludeRepos...)
	return out
}

// BoolDefault returns the value of a *bool, or the provided default if nil.
func BoolDefault(b *bool, def bool) bool {
	if b != nil {
		return *b
	}
	return def
}
