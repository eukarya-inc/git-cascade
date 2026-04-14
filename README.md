# git-cascade

A CLI tool that scans all repositories in a GitHub organization for compliance against a set of rules defined in YAML configuration files.

## Installation

```bash
go install github.com/eukarya-inc/git-cascade/cmd/git-cascade@latest
```

Or build from source:

```bash
go build -o git-cascade ./cmd/git-cascade
```

## Environment Variables

All parameters can be configured via environment variables. CLI flags take precedence when both are provided.

| Variable | Equivalent flag | Description |
|----------|----------------|-------------|
| `GIT_CASCADE_TOKEN` | `--token` | GitHub Personal Access Token |
| `GITHUB_TOKEN` | `--token` | GitHub PAT fallback (used if `GIT_CASCADE_TOKEN` is not set) |
| `GIT_CASCADE_APP_ID` | `--app-id` | GitHub App ID |
| `GIT_CASCADE_INSTALLATION_ID` | `--installation-id` | GitHub App Installation ID |
| `GIT_CASCADE_PRIVATE_KEY_PATH` | `--private-key-path` | Path to the GitHub App private key PEM file |
| `GIT_CASCADE_SLACK_WEBHOOK` | `--slack-webhook` | Slack Incoming Webhook URL |
| `GIT_CASCADE_SLACK_RESULTS_URL` | `--slack-results-url` | URL linked in the Slack notification (e.g. CI run URL) |
| `GIT_CASCADE_CONCURRENCY` | `--concurrency` | Number of concurrent (rule, repo) checks (default: 5) |

## Authentication

git-cascade supports two authentication methods: **Personal Access Token (PAT)** and **GitHub App**.

### Personal Access Token (PAT)

```bash
# Via environment variable (recommended)
export GIT_CASCADE_TOKEN=ghp_xxx

# Via CLI flag
git-cascade scan --org myorg --token ghp_xxx
```

### GitHub App

```bash
# Via environment variables (recommended)
export GIT_CASCADE_APP_ID=12345
export GIT_CASCADE_INSTALLATION_ID=67890
export GIT_CASCADE_PRIVATE_KEY_PATH=/path/to/key.pem

# Via CLI flags
git-cascade scan --org myorg \
  --app-id 12345 \
  --installation-id 67890 \
  --private-key-path key.pem
```

## Required Permissions

The table below lists the minimum permissions needed. Use the **read-only** column if you only run compliance checks. Add the **write** column if you use `--issue-mode` to post GitHub Issues.

### Personal Access Token (Classic)

| Scope | Read-only scans | With `--issue-mode` |
|-------|----------------|---------------------|
| `public_repo` | Public repos only | — |
| `repo` | Public + private repos | Required (includes Issues write) |

> `repo` is the only classic scope needed for all features.

### Fine-Grained Personal Access Token

| Permission | Access (read-only) | Access (with `--issue-mode`) |
|-----------|-------------------|------------------------------|
| Repository: Metadata | Read | Read |
| Repository: Contents | Read | Read |
| Repository: Administration | Read | Read |
| Repository: Members | Read | Read |
| Repository: Issues | — | Read & Write |

> Metadata is granted automatically and does not need to be configured explicitly.

### GitHub App

| Permission | Access (read-only) | Access (with `--issue-mode`) |
|-----------|-------------------|------------------------------|
| Repository: Metadata | Read | Read |
| Repository: Contents | Read | Read |
| Repository: Administration | Read | Read |
| Organization: Members | Read | Read |
| Repository: Issues | — | Read & Write |

The GitHub App must be installed on the organization with access to the repositories you want to scan. For `--issue-mode compliance`, the app also needs Issues write access on the compliance repository.

### Slack Notification

For `--slack-webhook`, no additional GitHub permissions are required. You only need a [Slack Incoming Webhook URL](https://api.slack.com/messaging/webhooks). Set `GIT_CASCADE_SLACK_WEBHOOK` to avoid putting the URL in config files.

### Permission per Check

| Check | API Endpoint | PAT Classic | Fine-Grained | GitHub App |
|-------|-------------|-------------|--------------|------------|
| `readme-exists` | `GET /repos/{owner}/{repo}/contents/{path}` | `repo` / `public_repo` | Contents: Read | Contents: Read |
| `license-exists` | `GET /repos/{owner}/{repo}/contents/{path}` | `repo` / `public_repo` | Contents: Read | Contents: Read |
| `codeowners-exists` | `GET /repos/{owner}/{repo}/contents/{path}` | `repo` / `public_repo` | Contents: Read | Contents: Read |
| `branch-protection` | `GET /repos/{owner}/{repo}/branches/{branch}/protection` | `repo` | Administration: Read | Administration: Read |
| `actions-pinned` | `GET /repos/{owner}/{repo}/contents/{path}` | `repo` / `public_repo` | Contents: Read | Contents: Read |
| `lockfile-required` | `GET /repos/{owner}/{repo}/contents/{path}` | `repo` / `public_repo` | Contents: Read | Contents: Read |
| `dockerfile-digest` | `GET /repos/{owner}/{repo}/contents/{path}` | `repo` / `public_repo` | Contents: Read | Contents: Read |
| `npm-ci-required` | `GET /repos/{owner}/{repo}/contents/{path}` | `repo` / `public_repo` | Contents: Read | Contents: Read |
| `renovate-config` | `GET /repos/{owner}/{repo}/contents/{path}` | `repo` / `public_repo` | Contents: Read | Contents: Read |
| `external-collaborators` | `GET /repos/{owner}/{repo}/collaborators` | `repo` | Members: Read | Members (Org): Read |
| `--issue-mode` | `GET/POST /repos/{owner}/{repo}/issues` | `repo` | Issues: Read & Write | Issues: Read & Write |

## Usage

```bash
# Scan all repos in an organization
git-cascade scan --org myorg

# Scan with JSON output
git-cascade scan --org myorg --format json

# Write SARIF output for GitHub Code Scanning upload
git-cascade scan --org myorg --format sarif --output results.sarif

# Write CSV to a file
git-cascade scan --org myorg --format csv --output findings.csv

# Scan only private repos
git-cascade scan --org myorg --skip-public

# Scan only specific repos
git-cascade scan --org myorg --include-repo api --include-repo web

# Exclude specific repos
git-cascade scan --org myorg --exclude-repo sandbox --exclude-repo archive

# Use local config instead of remote compliance repo
git-cascade scan --org myorg --local-config ./compliance/

# Notify Slack after scanning
git-cascade scan --org myorg --slack-webhook https://hooks.slack.com/services/xxx \
  --slack-results-url https://github.com/myorg/compliance/actions/runs/123

# Post a consolidated GitHub Issue with all findings
git-cascade scan --org myorg --issue-mode compliance

# Post one issue per failing repository
git-cascade scan --org myorg --issue-mode repo --issue-label compliance --issue-label automated

# Suppress progress logging (verbose is on by default)
git-cascade scan --org myorg --silent

# Limit concurrency to avoid GitHub secondary rate limits
git-cascade scan --org myorg --concurrency 3
```

## Configuration

Compliance rules are defined in YAML files. By default, git-cascade loads all `.yaml`/`.yml` files from the root of the `compliance` repository in your organization. Override with `--config-repo`, `--config-path`, or `--local-config`.

### Splitting config across multiple files

You can split your configuration across multiple files in the same directory — git-cascade merges them into a single config at load time:

- `version` — only needs to appear in one file; first file wins
- `scope`, `output`, `notify` — first file that sets each field wins
- `rules` — collected from all files (appended in filename order)

A typical layout:

```
compliance/         ← root of the compliance repository
  base.yaml         ← version, scope, output, notify
  governance.yaml   ← readme-exists, license-exists, codeowners-exists, external-collaborators
  security.yaml     ← branch-protection, actions-pinned, dockerfile-digest
  dependencies.yaml ← lockfile-required, npm-ci-required, renovate-config
```

Rule-only files (e.g. `security.yaml`) do not need a `version` field.

### Config Structure

```yaml
version: "1"

scope:
  include_public: true
  include_private: true
  include_archived: false
  include_repos:        # Only scan these repos (overrides all other scope filters)
    - api
    - web
  exclude_repos:        # Skip these repos
    - sandbox

output:
  format: table         # table | json | csv | sarif
  path: ""              # Write to this file; empty = stdout

notify:
  slack:
    enabled: false
    # webhook_url is better set via GIT_CASCADE_SLACK_WEBHOOK env var
    webhook_url: ""
    channel: ""         # Overrides the webhook's default channel
    # results_url is a runtime value — pass it via --slack-results-url flag
    # or GIT_CASCADE_SLACK_RESULTS_URL env var, not in the config file
  issues:
    enabled: false
    mode: compliance    # compliance = one consolidated issue | repo = one issue per failing repo
    compliance_repo: "" # owner/repo for mode=compliance; defaults to <org>/compliance
    labels:
      - compliance
      - automated

rules:
  - id: branch-protection
    name: Branch Protection
    description: Default branch must have branch protection rules enabled
    severity: error       # error | warning | info
    enabled: true
    params:               # Rule-specific parameters (optional)
      require_reviews: "true"
      required_reviewers: "1"
```

CLI flags always override the corresponding YAML config key when explicitly provided.

### Available Rules

| Rule ID | Description | Params |
|---------|-------------|--------|
| `readme-exists` | Repository must contain a README file | — |
| `license-exists` | Repository must contain a LICENSE file | — |
| `codeowners-exists` | CODEOWNERS must exist in `.github/`, root, or `docs/` | — |
| `branch-protection` | Default branch must have protection rules enabled; skipped for private repos on free GitHub plans | `require_reviews`, `required_reviewers` |
| `actions-pinned` | GitHub Actions in workflows must use pinned SHA refs instead of tags | — |
| `lockfile-required` | Package manifests must have corresponding lockfiles committed | — |
| `dockerfile-digest` | Dockerfile `FROM` images must use `@sha256:` digest pinning | — |
| `npm-ci-required` | CI workflows must use `npm ci` instead of `npm install` | — |
| `renovate-config` | Renovate config must extend shared preset with a cooldown | `extends`, `min_stability_days` |
| `external-collaborators` | No external collaborators may have admin privileges | — |

## Output Formats

| Format | Flag | Description |
|--------|------|-------------|
| `table` | default | Human-readable, tab-aligned terminal output |
| `json` | `--format json` | Machine-readable JSON array |
| `csv` | `--format csv` | Comma-separated values for spreadsheet import |
| `sarif` | `--format sarif` | SARIF 2.1.0 for [GitHub Code Scanning](https://docs.github.com/en/code-security/code-scanning) upload |

Use `--output <file>` to write results to a file. Without it, output goes to stdout.

**Table** (default) — results grouped by repository, sorted alphabetically:
```
org/api
───────
  STATUS  SEVERITY  RULE               MESSAGE
  ------  --------  ----               -------
  pass    warning   readme-exists      found README.md
  skip    error     branch-protection  branch protection API not available (requires GitHub Pro or public repository)

org/web
───────
  STATUS  SEVERITY  RULE               MESSAGE
  ------  --------  ----               -------
  pass    warning   readme-exists      found README.md
  fail    error     branch-protection  branch protection not enabled on main

```

**SARIF** — only failures are emitted. Upload with:
```bash
git-cascade scan --org myorg --format sarif --output results.sarif
gh api --method POST /repos/myorg/compliance/code-scanning/sarifs \
  --field commit_sha=$(git rev-parse HEAD) \
  --field ref=refs/heads/main \
  --field sarif=@results.sarif
```

## Notifications

### Slack

After a scan, git-cascade posts a summary with pass/warn/error counts and a breakdown of failures per repository.

```bash
# Via flag
git-cascade scan --org myorg --slack-webhook https://hooks.slack.com/services/xxx

# Via env var (recommended)
export GIT_CASCADE_SLACK_WEBHOOK=https://hooks.slack.com/services/xxx
git-cascade scan --org myorg
```

Use `--slack-results-url` to include a link to the full results (e.g. a CI run or uploaded SARIF artifact).

### GitHub Issues

git-cascade can create or update GitHub Issues with the full findings after each scan. Issues are upserted — re-running the scan updates the existing issue rather than creating duplicates.

**`--issue-mode compliance`** — one consolidated issue in `{org}/compliance` (or `--issue-repo owner/repo`), grouping all findings by repository.

**`--issue-mode repo`** — one issue per scanned repository that has failures, posted directly in that repository.

```bash
# Consolidated issue
git-cascade scan --org myorg --issue-mode compliance --issue-label compliance

# Per-repo issues
git-cascade scan --org myorg --issue-mode repo --issue-label compliance --issue-label automated
```

> Requires Issues: Read & Write permission (see [Required Permissions](#required-permissions)).

## Performance

Checks run concurrently with a default pool of **5 workers** (`--concurrency` / `GIT_CASCADE_CONCURRENCY`). Each `(rule, repo)` pair is an independent job, dispatched repo-first so all rules for a given repository are processed in parallel rather than exhausting one rule across all repos before starting the next.

5 workers is chosen to stay safely under GitHub's secondary rate limit of ~900 requests/minute per installation. If you hit rate limit errors, lower it further with `--concurrency 2` or `--concurrency 3`. Rate limit errors are automatically retried once after waiting for the reset window.

Files larger than 1 MB are streamed via the Git blob download API rather than the contents API, so large lockfiles (e.g. `package-lock.json`) are handled transparently.

## Result statuses

| Status | Meaning |
|--------|---------|
| `pass` | Check passed |
| `fail` | Check failed |
| `skip` | Check was skipped (e.g. `branch-protection` on a private repo under a free GitHub plan) |

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | All checks passed (or only `warning`/`info` severity failures) |
| `1` | One or more checks with `error` severity failed, or a runtime error occurred |

## License

MIT
