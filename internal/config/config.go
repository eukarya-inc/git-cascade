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
	WebhookURL string `yaml:"webhook_url,omitempty"`
	// Channel overrides the default channel configured on the webhook (optional).
	Channel string `yaml:"channel,omitempty"`
	// ResultsURL is not stored in config — it is always a runtime value supplied
	// via --slack-results-url flag or GIT_CASCADE_SLACK_RESULTS_URL env var.
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
// When omitted, defaults to scanning all public and private repos, excluding archived.
type Scope struct {
	IncludePublic  *bool    `yaml:"include_public,omitempty"`
	IncludePrivate *bool    `yaml:"include_private,omitempty"`
	IncludeArchived *bool   `yaml:"include_archived,omitempty"`
	IncludeRepos   []string `yaml:"include_repos,omitempty"`
	ExcludeRepos   []string `yaml:"exclude_repos,omitempty"`
}

// Rule defines a single compliance check.
type Rule struct {
	ID          string            `yaml:"id"`
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	Severity    Severity          `yaml:"severity"`
	Enabled     bool              `yaml:"enabled"`
	Params      map[string]string `yaml:"params,omitempty"`
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
