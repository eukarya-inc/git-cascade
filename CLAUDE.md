# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`git-cascade` is a Go CLI that scans every repository in a GitHub organization against compliance rules defined in YAML, then reports results (table/json/csv/sarif) and optionally posts findings to Slack and/or GitHub Issues.

## Commands

```bash
go build -o git-cascade ./cmd/git-cascade   # build (Makefile: make build)
go vet ./...                                 # lint (Makefile: make lint — no golangci-lint configured)
go test ./...                                 # test (Makefile: make test)
go test -race -coverprofile=coverage.out -covermode=atomic ./...  # matches CI exactly
go test ./internal/compliance/checks/... -run TestActionsPinned   # single test
go test ./internal/notify/... -run TestPostIssues/append_mode -v  # single subtest
```

CI (`.github/workflows/ci.yml`) runs `go build ./...`, `go vet ./...`, then the race-enabled coverage test above — match that when validating changes.

## Architecture

### Flow (entry point `cmd/git-cascade/cmd/scan.go: runScan`)

1. **Auth** — resolve `gh.Credentials` (PAT or GitHub App) via `resolveCredentials()`, build a `*github.Client` (`internal/github/auth.go`).
2. **Config** — load `config.ComplianceConfig` either from a local directory (`--local-config`) or from YAML files in an org's `compliance` repo (`compliance.LoadConfigFromRepo`, `internal/compliance/configloader.go`). Multiple YAML files in the same directory are merged: `version`/`scope`/`output`/`notify` use first-file-wins, `rules` are concatenated.
3. **Repo listing + filtering** — `gh.ListOrgRepos` then `gh.RepoFilterFromScope` (`internal/github/filter.go`), seeded from YAML `scope` and overridden by CLI flags only when explicitly `cmd.Flags().Changed(...)`.
4. **Engine.Run** (`internal/compliance/engine.go`) — builds `(rule, repo)` job pairs interleaved *repo-first* (all rules for one repo are adjacent) so a worker pool of `concurrency` goroutines (default 10, flag/env `--concurrency`/`GIT_CASCADE_CONCURRENCY`) doesn't exhaust one rule across all repos before starting the next. Per-repo `gh.RepoCache` (`internal/github/cache.go`) is injected via context so all checkers for the same repo share directory/file fetches (singleflight-style, keyed by owner/repo/ref/path).
5. **Output** — `internal/output` writes table/json/csv/sarif.
6. **Remediation** — `internal/remediation.Run` (see below) opens/updates fix PRs for failing results with auto-remediation enabled, before Slack/Issues so a future integration could link the fix PR into those notifications.
7. **Notify** — GitHub Issues (`internal/notify/issues.go`) run *before* Slack so the issue URL can be linked into the Slack message. Issues can be posted with separate (`--notify-*`) credentials from the scan itself, falling back to scan credentials when unset — this is what lets you scan org-a and post a compliance issue into org-b/compliance.
8. Non-zero exit if any `error`-severity result has `status: fail`.

### Checker plugin pattern (`internal/compliance/checker.go` + `internal/compliance/checks/*.go`)

Each rule ID maps to a `Checker` (`ID() string`, `Check(ctx, client, repo, rule) (*Result, error)`) registered via a package-level `init()` calling `compliance.Register(...)` into a global map. To add a new rule: create `internal/compliance/checks/<name>.go`, implement `Checker`, register it in `init()` — the blank import in `cmd/git-cascade/cmd/scan.go`'s import graph (via `internal/compliance/checks`) wires it up automatically. Look at `file_exists.go` (simplest) and `parallel.go` (fan-out helper for checks needing multiple API calls per repo) as templates.

Checkers must fetch file/directory content through `gh.CachedFetchFileContent` / `gh.CachedListDirectoryContents`, never the raw `gh.FetchFileContent`/`gh.ListDirectoryContents`, so the per-repo cache in the engine's worker pool is actually used.

### Suppression comments

Line-scanning checks (`secret-detection`, `no-secrets-inherit`, `no-pull-request-target`, `actions-pinned`, `dockerfile-digest`, `npm-ci-required`) honor a `git-cascade:allow` comment (in the language's comment syntax) on the same line or the line above — implemented once in `internal/compliance/checks/allow_comment.go` and reused by each checker rather than reimplemented per-rule.

### Config structure (`internal/config/config.go`)

`ComplianceConfig{Version, Scope, Output, Notify{Slack, Issues}, Remediation, Rules, IncludeRules, ExcludeRules}`. Rules can carry a per-rule `Scope` override (`config.RuleScope`) that the engine applies via `repoMatchesRuleScope` — this is a *narrowing* override only for that rule, separate from the top-level scan scope. Rules can also carry a per-rule `AutoRemediation *bool` — see Auto-remediation below.

### Auto-remediation (`internal/remediation/` + `internal/remediation/fixes/*.go`)

Mirrors the checker plugin pattern exactly: each fixable rule ID registers a `Remediator` (`ID() string`, `Remediate(ctx, client, repo, rule, result) (*Fix, error)`) via `remediation.Register(...)` in an `init()`, and `remediation.Run` (called from `runScan` after output write, before Issues/Slack) dispatches failing results to the matching one and opens/updates a PR. Gated at two levels: `remediation.enabled` is the master switch (nothing runs if false); `rule.AutoRemediation` (resolved via `config.EffectiveAutoRemediation`, default false) decides whether that specific rule is in scope. There is deliberately no block-level default — each rule opts in individually so a newly registered fixer never goes live silently just because remediation as a whole is already enabled. A rule with no registered `Remediator` is silently skipped even if `auto_remediation` is true on it.

Not every checker has a paired remediator — only rules where the fix is a deterministic file edit (see the "Auto-Remediation" section in `README.md` for the current list and the excluded-as-too-risky rationale). Detection logic that needs to be shared between a checker and its remediator (so they can never drift) is exported from the checker's file — e.g. `checks.FindUnpinnedActions`, `checks.FindNodeInstallViolations`, `checks.FindJobsMissingHardenRunner` — rather than re-derived from the checker's free-text `Result.Message`.

Git write operations live in `internal/github/write.go` (`EnsureBranch`, `CommitFiles`, `CreateOrUpdatePullRequest`) — separate from the read-only helpers in `repos.go`/`cache.go`. `CommitFiles` batches all of a `Fix`'s file changes into one commit via the Git Data API (blob → tree → commit → ref update) and is a no-op if the resulting tree is unchanged. `CreateOrUpdatePullRequest` upserts against an existing open PR with the same head branch rather than creating duplicates on repeat scans — if the title/body of that PR differ from the current fix (e.g. a remediator's PR title format changed), it edits them in place rather than leaving the existing PR untouched. `remediateOne` (`internal/remediation/engine.go`) always calls `CreateOrUpdatePullRequest` after `CommitFiles`, even when the commit was a no-op — otherwise a branch whose content already matches the fix (from an earlier run) would never get its stale PR title/body synced. Remediator `Fix.PRTitle` values follow Conventional Commits (`fix(<rule-id>): <description>`), matching the repo's commit message convention.

Remediation credentials (`--remediate-*` / `GIT_CASCADE_REMEDIATE_*`) have **no fallback** to scan or notify credentials — `resolveRemediateCredentials` errors out if `remediation.enabled` is true and none are set, since this is the one credential set that writes directly to scanned repositories.

### Notification modes (`internal/notify/issues.go`)

Three `--issue-mode` values, all upserting rather than duplicating:
- `compliance` — one consolidated issue (default repo `{org}/compliance`, override with `--issue-repo`), grouped by repository.
- `repo` — one issue per failing repository, posted in that repository.
- `append` — a single comment (marked with a hidden `git-cascade:section:<key>` marker so re-runs edit rather than duplicate) on a shared issue identified by `--issue-title`; `--issue-section-key` (default: org name) disambiguates multiple configs posting into the same shared issue.

### Credential resolution precedence

CLI flag > env var > (for GitHub App fields) partial-flag/env merge > `gh.CredentialsFromEnv()` fallback. This same shape (`credentialFlags` struct + `resolveCredentialsFrom`) is reused for scan, notify, and remediate credentials in `cmd/git-cascade/cmd/scan.go` — don't special-case any of their parsing separately from scan. The one difference: notify credentials fall back to scan credentials when unset (`resolveNotifyCredentials`); remediate credentials do not (`resolveRemediateCredentials` errors instead) because remediation writes directly to scanned repos rather than to a separate compliance/notify repo.

## Notes

- Full CLI flag reference, YAML config schema, rule catalog with API endpoints/permissions, and the secret-detection rule list all live in `README.md` — check there before re-deriving them.
- No `golangci-lint` config exists; `make lint` is just `go vet`.
