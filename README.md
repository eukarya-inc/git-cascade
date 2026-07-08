# git-cascade

[![CI](https://github.com/eukarya-inc/git-cascade/actions/workflows/ci.yml/badge.svg)](https://github.com/eukarya-inc/git-cascade/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/eukarya-inc/git-cascade/branch/main/graph/badge.svg)](https://codecov.io/gh/eukarya-inc/git-cascade)

A CLI tool that scans all repositories in a GitHub organization for compliance against a set of rules defined in YAML configuration files.

## Installation

### Homebrew (macOS / Linux)

```bash
brew tap eukarya-inc/tap
brew install git-cascade
```

### Go install

```bash
go install github.com/eukarya-inc/git-cascade/cmd/git-cascade@latest
```

### Build from source

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
| `GIT_CASCADE_SLACK_BOT_TOKEN` | `--slack-bot-token` | Slack bot user OAuth token (`xoxb-...`) |
| `GIT_CASCADE_SLACK_CHANNEL` | `--slack-channel` | Default Slack channel (webhook override or bot fallback) |
| `GIT_CASCADE_SLACK_RESULTS_URL` | `--slack-results-url` | URL linked in the Slack notification (e.g. CI run URL) |
| `GIT_CASCADE_ISSUE_MODE` | `--issue-mode` | Post findings as GitHub Issues: `compliance`, `repo`, or `append` |
| `GIT_CASCADE_ISSUE_REPO` | `--issue-repo` | `owner/repo` for consolidated or shared issue (mode=compliance\|append) |
| `GIT_CASCADE_ISSUE_HEADER` | `--issue-header` | Override the issue body heading (mode=compliance, default: `# Compliance Report — {org}`) |
| `GIT_CASCADE_ISSUE_TITLE` | `--issue-title` | Issue title (required for mode=append; overrides the default title for mode=compliance\|repo) |
| `GIT_CASCADE_ISSUE_SECTION_KEY` | `--issue-section-key` | Identifies this config's comment on a shared issue (mode=append, default: org) |
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

The GitHub App must be installed on the organization with access to the repositories you want to scan. For `--issue-mode compliance` or `--issue-mode append`, the app also needs Issues write access on the repository holding the issue (compliance repo, or `--issue-repo` for `append`).

### Slack Notification

No additional GitHub permissions are required for Slack notifications. See [Slack](#slack) for the two delivery methods and their setup.

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
| `no-env-files` | `GET /repos/{owner}/{repo}/contents/` (root listing) | `repo` / `public_repo` | Contents: Read | Contents: Read |
| `ai-config-safety` | `GET /repos/{owner}/{repo}/contents/{path}` | `repo` / `public_repo` | Contents: Read | Contents: Read |
| `no-pull-request-target` | `GET /repos/{owner}/{repo}/contents/{path}` | `repo` / `public_repo` | Contents: Read | Contents: Read |
| `no-secrets-inherit` | `GET /repos/{owner}/{repo}/contents/{path}` | `repo` / `public_repo` | Contents: Read | Contents: Read |
| `harden-runner-required` | `GET /repos/{owner}/{repo}/contents/{path}` | `public_repo` | Contents: Read | Contents: Read |
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

# Include forked repositories (excluded by default)
git-cascade scan --org myorg --include-forked

# Scan only specific repos
git-cascade scan --org myorg --include-repo api --include-repo web

# Exclude specific repos
git-cascade scan --org myorg --exclude-repo sandbox --exclude-repo archive

# Use local config instead of remote compliance repo
git-cascade scan --org myorg --local-config ./compliance/

# Notify Slack after scanning (webhook)
git-cascade scan --org myorg --slack-results-url https://github.com/myorg/compliance/actions/runs/123

# Post a consolidated GitHub Issue with all findings
git-cascade scan --org myorg --issue-mode compliance

# Post one issue per failing repository
git-cascade scan --org myorg --issue-mode repo --issue-label compliance --issue-label automated

# Append findings as a comment on a shared issue used by other scanning tools
git-cascade scan --org myorg --issue-mode append --issue-repo myorg/security --issue-title "Integrated Findings"

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
  security.yaml     ← branch-protection, actions-pinned, dockerfile-digest, no-env-files, ai-config-safety
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
  include_forked: false  # Exclude forked repositories (default: false)
  include_repos:         # Only scan these repos; overrides all other scope filters.
    - api                # Exact names and glob patterns are both supported.
    - web-*              # Matches web-frontend, web-admin, etc.
  exclude_repos:         # Skip these repos (glob patterns supported)
    - sandbox
    - legacy-*

# Restrict which rules run. Patterns are glob-matched against rule IDs.
# include_rules:         # When set, only matching rules run; exclude_rules is ignored.
#   - secret-*
#   - branch-protection
# exclude_rules:         # Skip matching rules.
#   - harden-runner-required

output:
  format: table         # table | json | csv | sarif
  path: ""              # Write to this file; empty = stdout

notify:
  slack:
    enabled: true
    # --- Delivery method (choose one) ---
    # Webhook — posts to a single fixed channel. Simplest option.
    webhook_url: ""     # prefer GIT_CASCADE_SLACK_WEBHOOK env var
    # Bot token — required for per-channel routing via repository_channels.
    bot_token: ""       # prefer GIT_CASCADE_SLACK_BOT_TOKEN env var
    # Default channel (webhook override, or bot fallback for unmapped repos)
    channel: "#compliance"
    # Per-repo routing (requires bot_token)
    repository_channels:
      - channels: "#ops, #security"   # one or more channels, comma-separated
        repositories: "api, backend"  # one or more repo names, comma-separated
      - channels: "#frontend"
        repositories: "web, dashboard"
    # results_url is a runtime value — pass via --slack-results-url flag
    # or GIT_CASCADE_SLACK_RESULTS_URL env var, not stored in config
  issues:
    enabled: false
    mode: compliance    # compliance = one consolidated issue | repo = one issue per failing repo | append = comment on a shared issue
    compliance_repo: "" # owner/repo for mode=compliance/append; defaults to <org>/compliance
    header_text: ""     # overrides the "# Compliance Report — {org}" heading (mode=compliance)
    issue_title: ""     # shared issue title (mode=append, required); overrides default issue title for mode=compliance/repo
    section_key: ""     # identifies this config's comment on the shared issue (mode=append, default: org)
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
      # Flat list — check these branches for every repo:
      # additional_branches:
      #   - develop
      #   - staging
      # Map format — check each branch only for the listed repos (owner/repo):
      additional_branches:
        development:
          - eukarya-inc/repository1
          - eukarya-inc/repository2
        securebranch:
          - eukarya-inc/repository3

  - id: actions-pinned
    name: Actions Pinned to SHA
    severity: error
    enabled: true
    scope:                # Per-rule scope overrides the top-level scope for this rule only.
      exclude_repos:      # Glob patterns matched against the short repo name.
        - testing
        - sandbox-*
      # include_repos takes precedence over exclude_repos when both are set:
      # include_repos:
      #   - super-important-repo
```

#### Rule-level scope

Each rule can define its own `scope` block to override the top-level scope **for that rule only**. This lets you skip noisy repos for one rule without changing the global scan target.

```yaml
rules:
  - id: harden-runner-required
    enabled: true
    severity: error
    scope:
      exclude_repos:
        - testing
        - sandbox-*

  - id: branch-protection
    enabled: true
    severity: error
    scope:
      include_repos:       # include_repos takes precedence — only these repos are checked.
        - critical-api
        - payments-service
```

Behaviour:
- When `include_repos` is set on a rule, only matching repositories are checked by that rule; `exclude_repos` is ignored.
- When only `exclude_repos` is set, all repositories except those matching the patterns are checked.
- Patterns use glob syntax (`*` matches any characters, `?` matches one character, `[abc]` matches a character class), matched against the **short repository name** (not `owner/repo`).
- A rule with no `scope` block inherits no restriction from this field; the top-level scope still determines which repos are in the scan.

CLI flags always override the corresponding YAML config key when explicitly provided.

### Filtering rules

Two top-level fields let you limit which rules run without editing individual rule entries.

**`include_rules`** — whitelist. When set, only rules whose IDs match at least one pattern run; `exclude_rules` is ignored.

**`exclude_rules`** — blacklist. Rules whose IDs match any pattern are skipped.

Patterns use glob syntax matched against the rule ID.

```yaml
# Run only secret-detection and branch-protection rules:
include_rules:
  - secret-*
  - branch-protection

# Or skip a noisy rule while keeping everything else:
exclude_rules:
  - generic-secret-assignment
  - harden-runner-required
```

These fields complement the per-rule `enabled: false` flag — use `enabled` to permanently disable a rule in config, and `include_rules`/`exclude_rules` for ad-hoc run-time filtering (e.g. running only security rules in a nightly job).

### Available Rules

| Rule ID | Description | Params |
|---------|-------------|--------|
| `readme-exists` | Repository must contain a README file | — |
| `license-exists` | Repository must contain a LICENSE file | — |
| `codeowners-exists` | CODEOWNERS must exist in `.github/`, root, or `docs/` | — |
| `branch-protection` | Default branch must have protection rules enabled; skipped for private repos on free GitHub plans | `require_reviews`, `required_reviewers`, `additional_branches` |
| `actions-pinned` | GitHub Actions in workflows must use pinned SHA refs instead of tags | — |
| `lockfile-required` | Package manifests must have corresponding lockfiles committed | — |
| `dockerfile-digest` | Dockerfile `FROM` images must use `@sha256:` digest pinning | — |
| `npm-ci-required` | CI workflows must use locked install commands (`npm ci`, `pnpm install --frozen-lockfile`, `yarn install --immutable`) instead of bare install commands | — |
| `renovate-config` | Renovate config must extend shared preset with a cooldown | `extends`, `min_stability_days` |
| `external-collaborators` | No external collaborators may have admin privileges | — |
| `no-env-files` | `.env`, `.env.local`, `.env.production` and other `.env.*` variants must not be committed (`.env.example` is allowed) | — |
| `ai-config-safety` | `.claude/`, `.cursor/`, and `.mcp.json` must not contain executable hooks or command definitions | — |
| `no-pull-request-target` | Workflows must not use `pull_request_target`, which runs in the base branch context and exposes secrets to untrusted fork code | — |
| `no-secrets-inherit` | Reusable workflow calls must not use `secrets: inherit`, which exposes all caller secrets violating least-privilege | — |
| `harden-runner-required` | Every job in public repository workflows must use `step-security/harden-runner` as the first step; skipped for private repositories | — |
| `secret-detection` | Scan all committed files for secrets: API keys, tokens, private keys, and credentials embedded in URLs | `rules`, `exclude_rules` |

#### Rule Params Reference

**`branch-protection`**

| Param | Type | Description |
|-------|------|-------------|
| `require_reviews` | `"true"` / `"false"` | Require pull request reviews before merging |
| `required_reviewers` | integer string, e.g. `"2"` | Minimum number of required approving reviewers (requires `require_reviews: "true"`) |
| `additional_branches` | flat list **or** branch→repos map | Extra branches to check beyond the default branch (see formats below) |

`additional_branches` supports two formats:

**Flat list** — every listed branch is checked for all repos in the scan:

```yaml
- id: branch-protection
  severity: error
  enabled: true
  params:
    require_reviews: "true"
    required_reviewers: "2"
    additional_branches:
      - develop
      - staging
```

**Map format** — each branch is checked only for the repos explicitly listed under it (value is a list of `owner/repo` full names). Use this when only a subset of repos have a given branch:

```yaml
- id: branch-protection
  severity: error
  enabled: true
  params:
    require_reviews: "true"
    required_reviewers: "2"
    additional_branches:
      development:
        - eukarya-inc/repository1
        - eukarya-inc/repository2
      securebranch:
        - eukarya-inc/repository3
        - eukarya-inc/repository4
```

**`renovate-config`**

| Param | Type | Description |
|-------|------|-------------|
| `extends` | string | Required preset name (default: `github>reearth/renovate-config`) |
| `min_stability_days` | integer string | Minimum `stabilityDays` value required in the Renovate config |

**`secret-detection`**

Scans every non-binary, non-vendored file in the repository for committed secrets. Uses the GitHub Git Trees API to retrieve the full file list in one call, then fetches and pattern-matches each file's content.

Files and directories that are never scanned: `vendor/`, `node_modules/`, `dist/`, `build/`, `.git/`, and common binary extensions (`.png`, `.zip`, `.exe`, etc.).

| Param | Type | Description |
|-------|------|-------------|
| `rules` | YAML list of strings | Run only these detection rule IDs. When omitted, all rules are active. |
| `exclude_rules` | YAML list of strings | Remove these rule IDs from the active set. Ignored when `rules` is also set. |

Built-in detection rules:

| Rule ID | What it detects |
|---------|----------------|
| `aws-access-key-id` | AWS access key IDs (`AKIA*`, `ASIA*`, `AROA*`, …) |
| `aws-secret-access-key` | 40-char AWS secret access keys preceded by a label |
| `github-token` | GitHub fine-grained PATs and OAuth tokens (`ghp_`, `gho_`, `ghs_`, `github_pat_`) |
| `github-classic-token` | 40-char hex GitHub classic PATs with label context |
| `slack-token` | Slack OAuth tokens (`xoxb-`, `xoxa-`, `xoxp-`, `xoxr-`, `xoxs-`, `xoxo-`) |
| `slack-webhook` | Slack Incoming Webhook URLs (`hooks.slack.com/services/…`) |
| `url-credentials` | Credentials embedded in URLs across protocols: `http/https`, `ftp/ftps`, `sftp`, `ssh`, `git`, `postgresql/postgres`, `mysql`, `mongodb/mongodb+srv`, `redis/rediss`, `amqp/amqps`, `smtp/smtps`, `ldap/ldaps` |
| `private-key` | PEM-encoded private keys (`-----BEGIN … PRIVATE KEY-----`) |
| `gcp-service-account-key` | GCP service account JSON key files (matched by `private_key_id` field, `.json` files only) |
| `stripe-secret-key` | Stripe live secret keys (`sk_live_…`) |
| `stripe-publishable-key` | Stripe live publishable keys (`pk_live_…`) |
| `sendgrid-api-key` | SendGrid API keys (`SG.…`) |
| `twilio-api-key` | Twilio API keys (`SK` + 32 hex chars) |
| `npm-auth-token` | npm auth tokens in `.npmrc` files (`_authToken=…`) |
| `ai-api-key` | AI provider API keys — OpenAI (`sk-proj-…`), Anthropic (`sk-ant-…`), and similar long-segment `sk-` keys |
| `ai-api-key-short-segment` | OpenRouter / 9router-style keys (`sk-<hex>-<alnum>-<hex>`) |
| `generic-secret-assignment` | Generic `password=`, `api_key=`, `secret=` assignments with values ≥16 chars |

```yaml
- id: secret-detection
  name: Secret Detection
  description: Scan repository files for committed secrets
  severity: error
  enabled: true
  params:
    # Run only specific rules (optional — omit to run all):
    rules:
      - aws-access-key-id
      - github-token
      - private-key

    # Or exclude noisy rules while keeping everything else:
    # exclude_rules:
    #   - generic-secret-assignment
```

> `generic-secret-assignment` has a higher false-positive rate than the other rules (it matches any quoted value ≥16 chars after a secret-like key name). Consider excluding it if you have many config files with long non-secret values, or tuning with `rules` to enable only the patterns relevant to your stack.

## Suppressing False Positives

For checks that scan file content line by line, you can suppress an individual violation by adding a `git-cascade:allow` comment on the same line as the flagged value, or on the line immediately above it.

The comment can use any of the following styles to cover common languages and file formats:

| Comment style | Languages / formats |
|---|---|
| `# git-cascade:allow` | Shell, Python, Ruby, YAML, `.env`, HCL/Terraform, Dockerfile |
| `// git-cascade:allow` | Go, JavaScript/TypeScript, Java, Rust, Swift, Kotlin, PHP, C/C++ |
| `-- git-cascade:allow` | SQL, Lua, Haskell |
| `<!-- git-cascade:allow` | HTML, XML |
| `/* git-cascade:allow` | CSS, C block comment start |

### Supported checks

| Check | Where to place the comment |
|---|---|
| `secret-detection` | Same line as the secret, or the line above (useful for multi-line constructs like PEM headers) |
| `no-secrets-inherit` | Same line as `secrets: inherit`, or the line above |
| `no-pull-request-target` | Same line as the `pull_request_target:` trigger, or the line above |
| `actions-pinned` | Same line as the `uses:` entry |
| `dockerfile-digest` | Same line as the `FROM` instruction |
| `npm-ci-required` | Same line as the `npm install` / `pnpm install` / `yarn` command |

### Examples

**.env file** — suppress a known non-secret value flagged by `secret-detection`:
```bash
# Intentional test fixture — not a real key
AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE # git-cascade:allow
```

**PEM file** — preceding-line form for multi-line constructs:
```
# git-cascade:allow
-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAK...
```

**Go source** — suppress a hardcoded value used only in tests:
```go
password = "supersecretpassword123456" // git-cascade:allow
```

**GitHub Actions workflow** — allow a trusted action pinned by tag instead of SHA:
```yaml
      - uses: actions/checkout@v4 # git-cascade:allow
```

**Dockerfile** — allow a base image that intentionally uses a tag:
```dockerfile
FROM node:20-alpine # git-cascade:allow
```

**Workflow** — allow `secrets: inherit` for an intentional internal reuse:
```yaml
    secrets: inherit # git-cascade:allow
```

**CI script in a workflow** — allow a one-off `npm install` step:
```yaml
      - run: npm install -g @myorg/cli # git-cascade:allow
```

> The suppression applies only to the annotated line in the annotated file. Other files or other lines in the same file are unaffected.

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
org/api [private]
─────────────────
  STATUS  SEVERITY  RULE               MESSAGE
  ------  --------  ----               -------
  pass    warning   readme-exists      found README.md
  skip    error     branch-protection  branch protection API not available (requires GitHub Pro or public repository)

org/web [public]
────────────────
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

After a scan, git-cascade posts a summary to Slack with pass/warn/error counts and a breakdown of failures per repository.

#### Notification format

Each notification includes a header with the org name and overall status, followed by a summary line:

```
✅ git-cascade compliance scan — myorg
30 checks: 24 passed, 4 warnings, 2 errors
```

When results are routed to a channel via `repository_channels`, the repositories mapped to that channel are listed below the summary:

```
✅ git-cascade compliance scan — myorg
30 checks: 24 passed, 4 warnings, 2 errors
repositories:
- myorg/api
- myorg/backend
```

Channels that receive unrouted results (via the fallback `channel` param, the webhook path, or a bot token with no `repository_channels`) get the summary line only — no repository list.

Two delivery methods are supported; choose one:

#### Webhook (simple, single channel)

The simplest option. One webhook URL posts a single summary to a fixed channel. The channel is configured on the webhook itself — use `channel` in config only to override it.

```bash
# Via env var (recommended — avoids storing the URL in config)
export GIT_CASCADE_SLACK_WEBHOOK=https://hooks.slack.com/services/xxx
git-cascade scan --org myorg

# Via CLI flag
git-cascade scan --org myorg --slack-webhook https://hooks.slack.com/services/xxx
```

```yaml
notify:
  slack:
    enabled: true
    webhook_url: https://hooks.slack.com/services/xxx
    channel: "#compliance"   # optional override of the webhook's default channel
```

#### Bot Token (flexible, per-channel routing)

A Slack bot user OAuth token (`xoxb-...`) uses the Slack Web API (`chat.postMessage`) and supports routing results for specific repositories to different channels.

```bash
# Via env var (recommended)
export GIT_CASCADE_SLACK_BOT_TOKEN=xoxb-xxx
git-cascade scan --org myorg

# Via CLI flag
git-cascade scan --org myorg --slack-bot-token xoxb-xxx
```

```yaml
notify:
  slack:
    enabled: true
    bot_token: xoxb-xxx          # prefer GIT_CASCADE_SLACK_BOT_TOKEN env var
    channel: "#compliance"       # fallback channel for repositories not in any mapping
    repository_channels:
      - channels: "#ops, #security"
        repositories: "api, backend"
      - channels: "#frontend"
        repositories: "web, dashboard"
```

**How routing works:**

- Results are grouped by repository. Each repository's results are sent to every channel it is mapped to (many-to-many).
- Repository names are matched against the full `owner/repo` value or just the short repo name — both work.
- Repositories not matched by any mapping fall back to `channel` (if set); if no default channel is set, their results are silently dropped.
- When `repository_channels` is not configured, a single summary of all results is sent to `channel`.

**Creating a Slack bot token:**

1. Go to [api.slack.com/apps](https://api.slack.com/apps) and create a new app.
2. Under **OAuth & Permissions**, add the `chat:write` scope.
3. Install the app to your workspace and copy the **Bot User OAuth Token** (`xoxb-...`).
4. Invite the bot to each channel it needs to post in (`/invite @your-bot`).

#### Results URL

Use `--slack-results-url` (or `GIT_CASCADE_SLACK_RESULTS_URL`) to include a link to the full report in the notification (e.g. a GitHub Actions run URL or an uploaded SARIF artifact).

```bash
git-cascade scan --org myorg \
  --slack-results-url https://github.com/myorg/compliance/actions/runs/123
```

### GitHub Issues

git-cascade can create or update GitHub Issues with the full findings after each scan. Issues are upserted — re-running the scan updates the existing issue rather than creating duplicates.

**`--issue-mode compliance`** — one consolidated issue in `{org}/compliance` (or `--issue-repo owner/repo`), grouping all findings by repository. The issue body heading defaults to `# Compliance Report — {org}`; override it with `--issue-header`. The issue title defaults to `[COMPLIANCE] non-compliant item findings`; override it with `--issue-title`.

**`--issue-mode repo`** — one issue per scanned repository that has failures, posted directly in that repository. The issue title defaults to `[COMPLIANCE] non-compliant item findings`; override it with `--issue-title`.

**`--issue-mode append`** — one comment on an existing (or bootstrapped) issue identified by `--issue-title`, without touching the rest of the issue body. This lets multiple scanning tools — or multiple git-cascade configs — report into the same integrated issue page, each owning a distinct comment.

```bash
# Consolidated issue
git-cascade scan --org myorg --issue-mode compliance --issue-label compliance

# Per-repo issues
git-cascade scan --org myorg --issue-mode repo --issue-label compliance --issue-label automated

# Append a comment to a shared issue used by other scanning tools
git-cascade scan --org myorg --issue-mode append \
  --issue-repo myorg/security \
  --issue-title "Integrated Findings"
```

`append` mode requires `--issue-title` (the exact title of the shared issue; created if it doesn't exist yet). Findings are posted as a single comment marked with a hidden `git-cascade:section:<key>` marker, so re-running the scan edits that same comment instead of piling up duplicates.

If more than one git-cascade config/org posts into the same shared issue, set `--issue-section-key` (or `section_key` in config) to a unique value per config — otherwise they default to the org name and distinct orgs won't collide, but two configs for the *same* org would overwrite each other's comment.

```bash
# Two configs sharing one issue, each owning its own comment
git-cascade scan --org myorg --issue-mode append --issue-repo myorg/security \
  --issue-title "Integrated Findings" --issue-section-key myorg-backend

git-cascade scan --org myorg --issue-mode append --issue-repo myorg/security \
  --issue-title "Integrated Findings" --issue-section-key myorg-frontend
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
