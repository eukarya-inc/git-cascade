package checks

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/eukarya-inc/git-cascade/internal/compliance"
	"github.com/eukarya-inc/git-cascade/internal/config"
	gh "github.com/eukarya-inc/git-cascade/internal/github"
	"github.com/google/go-github/v84/github"
)

// secretRule describes a single secret pattern to detect.
type secretRule struct {
	// id is a short identifier shown in violation messages.
	id string
	// pattern matches the secret value in file content.
	pattern *regexp.Regexp
	// fileFilter, when non-nil, restricts the rule to matching file paths only.
	// When nil the rule is applied to all text files.
	fileFilter *regexp.Regexp
	// ignore, when non-nil, suppresses a match that this function returns true for.
	// Use for rule-specific false-positive exclusions that are too narrow for the
	// global isPlaceholder check.
	ignore func(match string) bool
}

// secretRules is the set of built-in patterns, inspired by secretlint's rule set.
// Each pattern targets a well-known secret format; false-positive rate is kept
// low by requiring structural anchors (prefixes, separators, lengths) rather
// than catching arbitrary high-entropy strings.
var secretRules = []secretRule{
	{
		// AWS access key ID — always starts with AKIA or ASIA followed by 16 uppercase
		// alphanumeric chars.
		id:      "aws-access-key-id",
		pattern: regexp.MustCompile(`(?:AKIA|ASIA|AROA|AIDA|ANPA|ANVA|APKA)[A-Z0-9]{16}`),
	},
	{
		// AWS secret access key — 40-char base64url string preceded by a common label.
		id:      "aws-secret-access-key",
		pattern: regexp.MustCompile(`(?i)aws.{0,20}secret.{0,20}[=:]["']?\s*([A-Za-z0-9/+]{40})`),
	},
	{
		// GitHub fine-grained PATs and OAuth tokens (new format).
		id:      "github-token",
		pattern: regexp.MustCompile(`(?:ghp|gho|ghu|ghs|ghr|github_pat)_[A-Za-z0-9_]{36,255}`),
	},
	{
		// GitHub classic PATs — 40-char lowercase hex, required to appear after a
		// token-label context to avoid flagging SHA refs.
		id:      "github-classic-token",
		pattern: regexp.MustCompile(`(?i)(?:github|gh).{0,10}(?:token|pat|key|secret).{0,5}[=:]["']?\s*([0-9a-f]{40})\b`),
	},
	{
		// Slack bot / app / user / workspace OAuth tokens.
		// Covers: xoxb (bot), xoxa (app), xoxp (user), xoxr (refresh),
		//         xoxs (workspace), xoxo (obsolete but still seen in the wild).
		id:      "slack-token",
		pattern: regexp.MustCompile(`xox[baprso]-[0-9A-Za-z\-]{10,}`),
	},
	{
		// Slack webhook URLs.
		id:      "slack-webhook",
		pattern: regexp.MustCompile(`https://hooks\.slack\.com/services/T[A-Z0-9]+/B[A-Z0-9]+/[A-Za-z0-9]+`),
	},
	{
		// Credentials embedded in URLs across common protocols:
		//   http/https, ftp/ftps, sftp, ssh, git,
		//   postgresql/postgres, mysql, mongodb/mongodb+srv,
		//   redis/rediss, amqp/amqps, smtp/smtps, ldap/ldaps
		id:      "url-credentials",
		pattern: regexp.MustCompile(`(?i)(?:https?|ftps?|sftp|ssh|git|postgresql|postgres|mysql|mongodb(?:\+srv)?|rediss?|amqps?|smtps?|ldaps?)://[^/\s:@]{1,}:[^/\s:@]{3,}@[a-zA-Z0-9]`),
		ignore: func(match string) bool {
			lower := strings.ToLower(match)
			for _, p := range urlCredentialPlaceholders {
				if strings.Contains(lower, p) {
					return true
				}
			}
			return false
		},
	},
	{
		// PEM-encoded private keys of any type.
		id:      "private-key",
		pattern: regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |OPENSSH |PGP )?PRIVATE KEY-----`),
	},
	{
		// GCP service account JSON — identified by the "private_key_id" field present
		// in every downloaded service account key file.
		id:         "gcp-service-account-key",
		pattern:    regexp.MustCompile(`"private_key_id"\s*:\s*"[0-9a-f]{40}"`),
		fileFilter: regexp.MustCompile(`\.json$`),
	},
	{
		// Stripe live secret keys.
		id:      "stripe-secret-key",
		pattern: regexp.MustCompile(`sk_live_[0-9A-Za-z]{24,}`),
	},
	{
		// Stripe live publishable keys (lower risk but still a leak indicator).
		id:      "stripe-publishable-key",
		pattern: regexp.MustCompile(`pk_live_[0-9A-Za-z]{24,}`),
	},
	{
		// SendGrid API keys.
		id:      "sendgrid-api-key",
		pattern: regexp.MustCompile(`SG\.[A-Za-z0-9_\-]{22,}\.[A-Za-z0-9_\-]{43,}`),
	},
	{
		// Twilio account SID or auth token patterns.
		id:      "twilio-api-key",
		pattern: regexp.MustCompile(`SK[0-9a-fA-F]{32}`),
	},
	{
		// npm auth tokens stored in .npmrc files.
		id:         "npm-auth-token",
		pattern:    regexp.MustCompile(`_authToken\s*=\s*[A-Za-z0-9_\-\.]{8,}`),
		fileFilter: regexp.MustCompile(`(?:^|/)\.npmrc$`),
	},
	{
		// AI service API keys — long-segment style: OpenAI (sk-proj-<48>),
		// Anthropic (sk-ant-<version>-<95>), and other providers with a long
		// final segment (≥20 alphanumeric chars after the last separator).
		id:      "ai-api-key",
		pattern: regexp.MustCompile(`sk-[A-Za-z0-9]{2,}(?:[-_][A-Za-z0-9]{2,})*[-_][A-Za-z0-9]{20,}`),
	},
	{
		// AI service API keys — short-segment style: OpenRouter / 9router
		// (sk-<16hex>-<6alnum>-<8hex>) where every segment is short but there
		// are at least 3 dash-separated groups after "sk".
		id:      "ai-api-key-short-segment",
		pattern: regexp.MustCompile(`sk-[0-9a-f]{8,}-[A-Za-z0-9]{4,}-[0-9a-f]{6,}`),
	},
	{
		// Generic high-entropy password / secret assignments.
		// Matches lines like: password = "abc123...", SECRET_KEY="...", etc.
		// The value must be at least 16 chars and not look like a common placeholder.
		id:      "generic-secret-assignment",
		pattern: regexp.MustCompile(`(?i)(?:password|passwd|secret|api_key|apikey|auth_token|authtoken|access_token|accesstoken)\s*[=:]\s*["']([^"'\s]{16,})["']`),
	},
}

// skipExtensions lists binary and non-text file extensions that are never scanned.
var skipExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".ico": true, ".svg": true, ".woff": true, ".woff2": true, ".ttf": true,
	".eot": true, ".mp4": true, ".mp3": true, ".wav": true, ".zip": true,
	".tar": true, ".gz": true, ".bz2": true, ".7z": true, ".rar": true,
	".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
	".exe": true, ".dll": true, ".so": true, ".dylib": true, ".bin": true,
	".pyc": true, ".class": true, ".o": true, ".a": true,
}

// skipPaths lists path prefixes that are never scanned (vendored / generated code).
var skipPaths = []string{
	"vendor/",
	"node_modules/",
	".git/",
	"dist/",
	"build/",
}

// placeholderPrefixes are strings that, when the matched value starts with them,
// indicate an obvious template / documentation value rather than a real secret.
var placeholderPrefixes = []string{
	"your-",
	"<",
	"todo",
	"fixme",
	"changeme",
	"placeholder",
	"insert-",
	"replace-",
	"put-your-",
}

// placeholderExact are exact match strings (case-insensitive) for well-known
// documentation values such as the canonical AWS example credentials.
var placeholderExact = []string{
	"xxxxxxxxxxxxxxxxxxx", // generic x-padding
	"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
}

// urlCredentialPlaceholders are substrings that, when found in a URL-credential
// match, indicate the URL is a documentation/example rather than a real secret.
var urlCredentialPlaceholders = []string{
	"user:pass@",
	"user:password@",
	"username:pass@",
	"username:password@",
	":password@",
	":secret@",
	":token@",
}

type secretDetectionChecker struct{}

func (c *secretDetectionChecker) ID() string { return "secret-detection" }

func (c *secretDetectionChecker) Check(ctx context.Context, client *github.Client, repo gh.Repository, rule config.Rule) (*compliance.Result, error) {
	activeRules := resolveActiveRules(rule)
	if len(activeRules) == 0 {
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusSkip,
			Severity: rule.Severity,
			Message:  "no secret rules enabled",
		}, nil
	}

	violations, err := c.scanRepoArchive(ctx, client, repo, activeRules)
	if err != nil {
		return nil, err
	}
	if violations == nil {
		// nil (not empty slice) signals that the repo HEAD could not be resolved.
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusSkip,
			Severity: rule.Severity,
			Message:  "could not resolve HEAD commit",
		}, nil
	}

	if len(violations) > 0 {
		return &compliance.Result{
			RuleID:   rule.ID,
			RuleName: rule.Name,
			Repo:     repo.FullName,
			Status:   compliance.StatusFail,
			Severity: rule.Severity,
			Message:  fmt.Sprintf("%d potential secret(s) detected: %s", len(violations), strings.Join(violations, "; ")),
		}, nil
	}

	return &compliance.Result{
		RuleID:   rule.ID,
		RuleName: rule.Name,
		Repo:     repo.FullName,
		Status:   compliance.StatusPass,
		Severity: rule.Severity,
		Message:  "no secrets detected",
	}, nil
}

const archiveMaxRetries = 3

// scanRepoArchive downloads the repo as a gzipped tarball (single API call) and
// scans each file's content against activeRules.
// Returns nil violations (not an empty slice) when HEAD cannot be resolved.
func (c *secretDetectionChecker) scanRepoArchive(ctx context.Context, client *github.Client, repo gh.Repository, activeRules []secretRule) ([]string, error) {
	archiveURL, _, err := client.Repositories.GetArchiveLink(
		ctx, repo.Owner, repo.Name,
		github.Tarball,
		&github.RepositoryContentGetOptions{Ref: repo.DefaultBranch},
		3,
	)
	if err != nil {
		// A 404 on the archive endpoint means the repo or branch doesn't exist.
		return nil, nil
	}

	var lastErr error
	for attempt := range archiveMaxRetries {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}

		violations, err := c.tryReadArchive(ctx, archiveURL.String(), repo, activeRules)
		if err == nil {
			return violations, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func (c *secretDetectionChecker) tryReadArchive(ctx context.Context, archiveURL string, repo gh.Repository, activeRules []secretRule) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building archive request for %s: %w", repo.FullName, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading archive for %s: %w", repo.FullName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("archive download for %s returned HTTP %d", repo.FullName, resp.StatusCode)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("opening gzip stream for %s: %w", repo.FullName, err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)

	// Use an empty slice (not nil) so the caller can distinguish "scanned, clean" from "HEAD not found".
	violations := []string{}

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading archive for %s: %w", repo.FullName, err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		// GitHub tarballs wrap everything in a top-level directory like
		// "owner-repo-sha/". Strip that prefix to get the repo-relative path.
		filePath := stripArchivePrefix(hdr.Name)
		if shouldSkipPath(filePath) {
			continue
		}

		content, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("reading file %s from archive of %s: %w", filePath, repo.FullName, err)
		}

		text := string(content)
		for _, sr := range activeRules {
			if sr.fileFilter != nil && !sr.fileFilter.MatchString(filePath) {
				continue
			}
			matches := sr.pattern.FindAllString(text, -1)
			for _, m := range matches {
				if isPlaceholder(m) {
					continue
				}
				if sr.ignore != nil && sr.ignore(m) {
					continue
				}
				violations = append(violations, fmt.Sprintf("%s (%s)", filePath, sr.id))
				break // one violation per rule per file is enough
			}
		}
	}

	return violations, nil
}

// stripArchivePrefix removes the top-level directory that GitHub wraps tarballs
// in (e.g. "owner-repo-abc1234/path/to/file" → "path/to/file").
func stripArchivePrefix(name string) string {
	if _, after, ok := strings.Cut(name, "/"); ok {
		return after
	}
	return name
}

// resolveActiveRules returns the subset of secretRules to apply.
// When the rule param "rules" is set, only those IDs are enabled.
// When "exclude_rules" is set, those IDs are removed from the full set.
func resolveActiveRules(rule config.Rule) []secretRule {
	if ids, ok := rule.ListParams["rules"]; ok && len(ids) > 0 {
		enabled := toStringSet(ids)
		var out []secretRule
		for _, sr := range secretRules {
			if enabled[sr.id] {
				out = append(out, sr)
			}
		}
		return out
	}

	if excluded, ok := rule.ListParams["exclude_rules"]; ok && len(excluded) > 0 {
		skip := toStringSet(excluded)
		var out []secretRule
		for _, sr := range secretRules {
			if !skip[sr.id] {
				out = append(out, sr)
			}
		}
		return out
	}

	return secretRules
}

// shouldSkipPath returns true if the file should not be scanned.
func shouldSkipPath(filePath string) bool {
	for _, prefix := range skipPaths {
		if strings.HasPrefix(filePath, prefix) {
			return true
		}
	}
	ext := fileExt(filePath)
	return skipExtensions[ext]
}

// isPlaceholder returns true if the matched string looks like example/template text.
func isPlaceholder(match string) bool {
	lower := strings.ToLower(match)
	for _, p := range placeholderExact {
		if lower == strings.ToLower(p) {
			return true
		}
	}
	for _, p := range placeholderPrefixes {
		if strings.HasPrefix(lower, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

// fileExt returns the lowercase file extension including the leading dot,
// or an empty string if there is none.
func fileExt(path string) string {
	for i := len(path) - 1; i >= 0 && path[i] != '/'; i-- {
		if path[i] == '.' {
			return strings.ToLower(path[i:])
		}
	}
	return ""
}

// toStringSet converts a slice of strings to a set (map[string]bool).
func toStringSet(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, item := range items {
		m[item] = true
	}
	return m
}

func init() {
	compliance.Register(&secretDetectionChecker{})
}
