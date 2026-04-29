# codehealth Integration Playbook

Drop-in recipe for wiring [`nellcorp/codehealth`](https://github.com/nellcorp/codehealth) — a single Go binary that exposes CodeScene + Codecov tooling via an MCP server (for Claude Code) and a CLI (for Lefthook + Make targets) — into any repository.

This playbook captures the integration applied to `nellcorp/hip`. Copy verbatim; only the project-specific values (CodeScene project ID, Codecov repo slug, threshold floors) need editing.

---

## What you get

- **MCP server** registered in `.mcp.json`, consumed by Claude Code. Tools:
  - CodeScene: `health_overview`, `list_hotspots`, `file_health`, `delta_check`, `score_file`
  - Codecov: `coverage_overview`, `file_coverage`, `delta_coverage`
- **CLI subcommands** for Lefthook + Make: `codehealth health`, `codehealth coverage`, `codehealth delta --staged`, `codehealth hotspots`, `codehealth file`, `codehealth coverage-file`, `codehealth coverage-delta`.
- **Pre-commit Lefthook hook** that surfaces complexity regressions on staged Go files. Warn-only — never blocks a commit.
- **Two slash commands** for Claude Code (`/health-check`, `/refactor-hotspots`).
- **Skip-on-missing semantics** — every entry-point silently no-ops when the binary is absent. CI gates remain authoritative.

---

## Prerequisites

- A CodeScene project (free for OSS, Pro for private). Note the numeric project ID from the URL `codescene.io/projects/<ID>`.
- A Codecov account with the repo activated. Note the slug `service/owner/repo` (e.g. `github/nellcorp/hip`).
- `codehealth` installed globally on every developer's `PATH` (and on CI runners that need it). The repo does **not** install it.
  ```bash
  go install github.com/nellcorp/codehealth/cmd/codehealth@latest
  # Or download a release binary from https://github.com/nellcorp/codehealth/releases
  ```
- `Lefthook` installed locally for the pre-commit hook (`brew install lefthook` or `go install github.com/evilmartians/lefthook@latest`).
- (Optional) [CodeScene `cs` CLI](https://codescene.io/docs/guides/cli/index.html) for engine-accurate local scoring. Without it, `codehealth` falls back to gocyclo+gocognit and prints a one-line stderr warning.

---

## Secrets

Configure as org-level (preferred for multi-repo orgs) or repo-level:

| Secret | Purpose |
|---|---|
| `CODESCENE_TOKEN` | CodeScene API auth (Personal Access Token, Account → API Tokens) |
| `CODESCENE_PROJECT_ID` | Numeric project ID |
| `CODECOV_TOKEN` | Codecov **Personal API token** (mint at `app.codecov.io/account/<service>/<user>/access`). The repo upload token used by `codecov-action` is **not** the same — the v2 REST API rejects upload tokens with `401 Invalid token`. |
| `CODECOV_REPO` | Slug `service/owner/repo`, e.g. `github/nellcorp/hip` |

For local development export them in your shell or via a non-committed `.env` file.

---

## File map

```
.mcp.json                                  ← register codehealth MCP server
.codescene-thresholds                      ← HOTSPOT_THRESHOLD + AVERAGE_THRESHOLD (ratcheted)
.codescenerc                               ← include / exclude paths
.codesceneignore                           ← paths CodeScene should ignore entirely
.codecov.yml                               ← per-flag config (carryforward, ignore globs)
.coverage-thresholds                       ← BACKEND_MIN (local floor)
.claude/commands/health-check.md           ← /health-check slash command
.claude/commands/refactor-hotspots.md      ← /refactor-hotspots slash command
docs/adr/NNNN-codehealth-integration.md    ← record the decision
lefthook.yml                               ← pre-commit + commit-msg hooks
Makefile                                   ← health-check + coverage-health targets
.github/workflows/code-health.yaml         ← CI threshold gate
```

---

## Drop-in snippets

### `.mcp.json`

Replace `<PROJECT_ID>` with the CodeScene project ID and `<SERVICE>/<OWNER>/<REPO>` with the Codecov slug.

```json
{
  "mcpServers": {
    "codehealth": {
      "command": "codehealth",
      "args": ["serve"],
      "env": {
        "CODESCENE_URL": "https://api.codescene.io",
        "CODESCENE_TOKEN": "${CODESCENE_TOKEN}",
        "CODESCENE_PROJECT_ID": "<PROJECT_ID>",
        "CODECOV_URL": "https://api.codecov.io",
        "CODECOV_TOKEN": "${CODECOV_TOKEN}",
        "CODECOV_REPO": "<SERVICE>/<OWNER>/<REPO>"
      }
    }
  }
}
```

### `Makefile` targets

```makefile
.PHONY: health-check coverage-health

# Print live CodeScene scores and the threshold floor.
# Requires the codehealth binary on PATH and CODESCENE_TOKEN +
# CODESCENE_PROJECT_ID in the environment. Skipped silently if codehealth
# is not installed.
health-check:
	@if command -v codehealth >/dev/null 2>&1; then \
		codehealth health; \
	else \
		echo "codehealth not on PATH — skipping"; \
	fi

# Print Codecov project coverage and the threshold floor.
# Requires the codehealth binary on PATH and CODECOV_TOKEN + CODECOV_REPO
# in the environment. Skipped silently if codehealth is not installed.
coverage-health:
	@if command -v codehealth >/dev/null 2>&1; then \
		codehealth coverage; \
	else \
		echo "codehealth not on PATH — skipping"; \
	fi
```

Multi-line `if/else` is required — make recipes execute one line per shell. A single-line `if` followed by `codehealth health` would still run when the binary is missing.

### `lefthook.yml` — append to existing `pre-commit` block

```yaml
pre-commit:
  parallel: true
  commands:
    codehealth-delta:
      tags: health
      glob: "*.go"
      run: |
        if ! command -v codehealth >/dev/null 2>&1; then
          exit 0
        fi
        codehealth delta --staged || true
```

`|| true` makes the hook warn-only — a non-zero exit from `codehealth delta` never blocks the commit.

### `.claude/commands/health-check.md`

```markdown
---
description: Print project CodeScene scores, Codecov coverage, threshold floors, and a one-line verdict.
---

Use the `codehealth` MCP server to report current code health for this repo:

1. Call `health_overview`. Show:
   - `hotspot.now` and `average.now` from CodeScene
   - `HOTSPOT_THRESHOLD` and `AVERAGE_THRESHOLD` from `.codescene-thresholds`
   - one-line verdict: passing / failing the gate
2. Call `list_hotspots` with `limit=5`. Print path + health for each.
3. Call `coverage_overview`. Show:
   - project coverage % from Codecov
   - `BACKEND_MIN` from `.coverage-thresholds`
   - one-line verdict: above / below floor
4. If any local Go files are staged, call `delta_check` with `staged=true` and report whether the staged change is net-positive, neutral, or net-negative.

Stay terse. The user wants a quick snapshot, not a refactor session.
```

### `.claude/commands/refactor-hotspots.md`

```markdown
---
description: Walk through the top CodeScene hotspots and refactor the worst, validating with delta_check.
---

Run a focused refactor session against the top hotspots in this repo.

Workflow:

1. Call `health_overview` so we know the current floor and live scores.
2. Call `list_hotspots` with `limit=5`. Pick the worst file by `health` (lowest score).
3. For that file:
   a. Call `file_health` to see biomarkers (deep nesting, long functions, complex conditionals).
   b. Read the file. Locate the offending function(s) using line ranges from biomarkers.
   c. Propose a refactor that addresses the worst smell without changing behavior.
      Prefer extracting helpers, flattening nesting, replacing nested conditionals with
      early returns. Do NOT rename public APIs unless asked.
   d. Show the diff. Wait for confirmation before applying.
4. After the user confirms, apply the edit, then call `delta_check` with `paths=[that file]`
   and report the before/after delta. Optionally call `file_coverage` for the same path
   to confirm coverage did not drop.
5. Run `make lint` and `make test` to confirm no regression.
6. Stop after one hotspot. Ask the user whether to proceed to the next one.

Constraints:
- Behavior MUST stay identical. If a refactor risks behavior change, surface the concern instead of editing.
- One file per pass. Do not bundle unrelated cleanups.
- Local `delta_check` is advisory; the CI gate (the threshold-enforcing workflow) is authoritative. If the change builds and tests pass but `delta_check` regresses, surface that and ask the user before continuing.
```

### `.codescene-thresholds`

Two-phase rollout: land in *warn* mode with placeholder values, run the workflow once, read measured baseline from CI logs, then update + flip to *enforce*.

```bash
# Ratcheted thresholds for the CodeScene quality gate.
# Lowering these requires a dedicated PR and a superseding ADR — refactor instead.
HOTSPOT_THRESHOLD=0.0
AVERAGE_THRESHOLD=0.0
```

### `.coverage-thresholds`

```bash
# Local floor for `make coverage-check`. Round down measured baseline.
BACKEND_MIN=0
```

### `.github/workflows/code-health.yaml`

CI gate calling the CodeScene REST API directly (not the binary, so no install step required on runners).

```yaml
name: Code Health

on:
  pull_request:
    branches: [main, development]
  push:
    branches: [main]

permissions:
  contents: read

jobs:
  health:
    name: CodeScene gate
    runs-on: ubuntu-latest
    env:
      CODESCENE_GATE_MODE: enforce   # warn during phase-1 baseline read; flip to enforce after
      CODESCENE_TOKEN: ${{ secrets.CODESCENE_TOKEN }}
      CODESCENE_PROJECT_ID: ${{ secrets.CODESCENE_PROJECT_ID }}
    steps:
      - uses: actions/checkout@v5

      - name: Fetch live scores from CodeScene API
        id: scores
        run: |
          set -euo pipefail
          if [[ -z "${CODESCENE_TOKEN:-}" || -z "${CODESCENE_PROJECT_ID:-}" ]]; then
            echo "CODESCENE_TOKEN or CODESCENE_PROJECT_ID not set — skipping" | tee -a "$GITHUB_STEP_SUMMARY"
            exit 0
          fi
          response=$(curl -sSf \
            -H "Authorization: Bearer $CODESCENE_TOKEN" \
            -H "Accept: application/json" \
            "https://api.codescene.io/v2/projects/$CODESCENE_PROJECT_ID")

          hotspot=$(echo "$response" | jq -r '.analysis.hotspot_code_health.now // empty')
          average=$(echo "$response" | jq -r '.analysis.code_health.now // empty')

          if [[ -z "$hotspot" || -z "$average" ]]; then
            echo "API response missing scores. Raw payload follows:" >&2
            echo "$response" >&2
            exit 1
          fi

          echo "hotspot=$hotspot" >> "$GITHUB_OUTPUT"
          echo "average=$average" >> "$GITHUB_OUTPUT"

      - name: Enforce thresholds
        if: steps.scores.outputs.hotspot != '' && steps.scores.outputs.average != ''
        run: |
          set -euo pipefail
          # shellcheck disable=SC1091
          source .codescene-thresholds
          hotspot="${{ steps.scores.outputs.hotspot }}"
          average="${{ steps.scores.outputs.average }}"

          fail=0
          if awk "BEGIN { exit !($hotspot < $HOTSPOT_THRESHOLD) }"; then
            echo "::error::hotspot code health $hotspot < threshold $HOTSPOT_THRESHOLD"
            fail=1
          fi
          if awk "BEGIN { exit !($average < $AVERAGE_THRESHOLD) }"; then
            echo "::error::average code health $average < threshold $AVERAGE_THRESHOLD"
            fail=1
          fi

          if [[ $fail -eq 1 ]]; then
            if [[ "${CODESCENE_GATE_MODE:-enforce}" == "warn" ]]; then
              echo "::warning::CodeScene gate in WARN mode — would fail under enforce."
              exit 0
            fi
            echo "Lowering .codescene-thresholds requires a dedicated PR and a superseding ADR."
            exit 1
          fi
          echo "OK: hotspot $hotspot >= $HOTSPOT_THRESHOLD, average $average >= $AVERAGE_THRESHOLD"
```

### `.env.example` snippet

```bash
# Code Health MCP (codehealth)
# Required for the .mcp.json codehealth server and for `make health-check` /
# `make coverage-health`. Tokens are per-developer; do not commit real values.
# CODESCENE_TOKEN=...                    # CodeScene Personal Access Token
# CODECOV_TOKEN=...                      # Codecov Personal API token (NOT the upload token)
```

### ADR template

Drop into `docs/adr/NNNN-codehealth-integration.md`:

```markdown
---
type: ADR
id: "NNNN"
title: "codehealth integration for pre-commit code-health + coverage signals"
status: active
date: YYYY-MM-DD
---

## Context

CI gates score code only after push: developers (and Claude Code) author code that regresses the score and only learn at PR time. We want the same feedback locally, before commit, for both code-health (CodeScene) and coverage (Codecov).

## Decision

Adopt `nellcorp/codehealth` — a portable Go binary that exposes both surfaces over MCP (for Claude Code) and a CLI (for Lefthook + Make targets). Local scoring uses CodeScene `cs` CLI when present, with a pure-Go fallback (gocyclo + gocognit) otherwise. Codecov tools are read-only against the v2 REST API — no local fallback (coverage requires an uploaded report).

The Lefthook entry runs `codehealth delta --staged` on `*.go` commits and is **warn-only** — never blocks a commit. The CI gates remain authoritative.

`codehealth` is installed globally on developer machines (not vendored by this repo). Every entry-point (Make targets, Lefthook hook, MCP server) skips silently when the binary is missing.

## Alternatives considered

- **In-house Python MCP server** — adds a per-developer Python runtime dependency. The single Go binary has no install footprint beyond `go install` or a release download.
- **Wrap `cs` CLI alone, no MCP** — leaves Claude with no structured tool surface.
- **Hard-fail pre-commit on local regression** — the local heuristic disagrees with CodeScene often enough that hard-failing creates toil unrelated to the real gate.
- **Pin `codehealth` version** — deferred until a breaking release lands. `@latest` mirrors policy of other dev-tool deps.

## Consequences

- One additional binary on developer machines (`codehealth`), installed globally — not vendored. Without it the local layer is silent.
- Two scoring engines coexist: CodeScene's analyzer (REST + `cs` CLI) and the pure-Go heuristic. Engines disagree at the margins; acceptable because the local layer is advisory.
- Claude Code gains structured access to both code-health and coverage during planning and refactor sessions.
- Codecov **Personal API token** is required (not the repo upload token). Document this clearly in onboarding — the codecov-action's `CODECOV_TOKEN` is a different secret type and the v2 API will reject it with `401 Invalid token`.

## Advice

- Treat `delta_check` and `coverage_overview` output as **leading indicators**, not gates. CI remains authority.
- When `delta_check` flags a regression, refactor the change rather than tweaking the threshold.
- A healthy file with poor coverage is a different fix than a poor-health file with good coverage. Use `file_health` + `file_coverage` together during refactor sessions.
```

---

## Two-phase rollout (the load-bearing trick)

This is what makes the gate stick.

### Phase 1 — Print mode

Land the framework with `HOTSPOT_THRESHOLD=0.0`, `AVERAGE_THRESHOLD=0.0`, `BACKEND_MIN=0`, and `CODESCENE_GATE_MODE: warn` in the workflow. Open a real PR. The step summary prints live scores. Read the numbers from CI logs.

### Phase 2 — Seed + flip

Open a second PR. Update `.codescene-thresholds` and `.coverage-thresholds` with the measured baselines (round coverage down). Drop the `CODESCENE_GATE_MODE` override (or set it to `enforce`). Merge.

From here every PR that drops the score fails. Lowering thresholds requires a dedicated PR and a superseding ADR — refactor instead.

---

## Verification

After landing the wiring:

```bash
# Without the binary on PATH — every entry-point should skip cleanly.
PATH=/usr/bin:/bin make health-check     # prints "codehealth not on PATH — skipping", exit 0
PATH=/usr/bin:/bin make coverage-health  # same
git commit ...                           # Lefthook codehealth-delta hook silently no-ops

# With the binary present + tokens set:
export CODESCENE_TOKEN=... CODECOV_TOKEN=...
make health-check        # prints CodeScene scores + threshold verdict
make coverage-health     # prints Codecov coverage + floor verdict
codehealth serve         # MCP server lists 8 tools (5 CodeScene + 3 Codecov)

# Smoke-test the slash commands in Claude Code:
/health-check            # snapshot of scores + coverage + hotspots + optional staged delta
/refactor-hotspots       # guided refactor against worst hotspot
```

---

## Known issues / gotchas

1. **Codecov auth scheme.** `codehealth` sends `Authorization: Bearer <token>`. Use a **Personal API token** minted at `app.codecov.io/account/<service>/<user>/access` — the repo *upload* token used by `codecov-action` is a different secret and the v2 API rejects it with `401 Invalid token`. Note: v0.2.1 briefly switched to `Authorization: Token` based on a misread of Codecov docs; that scheme rejects Personal API tokens in practice. Use v0.2.0 or v0.2.2+.
2. **Tagged release lag.** If `go install …@latest` resolves to a tag that predates the rename (e.g. v0.1.x of `codehealth` still ships `cmd/codescene-mcp`), pin to `@main` until v0.2.0+ is tagged, or download a release binary directly.
3. **Activate the repo on Codecov first.** Coverage tools 404 on repos not yet activated. Push at least one coverage report (via `codecov-action` in a CI workflow) before relying on `coverage_overview`.
4. **Sensitive-file gate.** Some Claude Code harnesses treat `.mcp.json` and `.claude/commands/*.md` as sensitive and prompt regardless of allowlist. Approve once per file or use `bypassPermissions` mode for the session.

---

## Why ratcheted thresholds beat absolute thresholds

A "must-be-above-9.0" gate gets dismissed the first time a bad afternoon drifts the score to 8.95. A ratcheted gate is the score the codebase actually had on the day it last passed. Lowering it is conscious; raising it is normal. The same logic applies to coverage — the floor is whatever was measured most recently, rounded down. Ratchet upward as PRs land.

---

## When the gate fails

Right response is *not* "lower the threshold". It is one of:

1. **Code is genuinely complex but adds value** — refactor before merging. Extract helpers, flatten nesting, split functions.
2. **Regression is incidental** (a generated file got newly counted, etc.) — adjust `.codecov.yml` ignores or `.codesceneignore`. Document why in the PR.
3. **Regression is real and accepted** — write an ADR superseding the threshold-gate ADR, explaining why. Load-bearing decision; deserves the ceremony.

---

## Maintenance cadence

- **Weekly** — review the top hotspot list (`/health-check` in Claude Code, or `codehealth hotspots --limit 10`). Top 3 dictate next refactor priority. `/refactor-hotspots` drives a guided session.
- **Per release** — ratchet thresholds upward when scores have improved. One-line PR, no ADR for tightening.
- **Per architectural change** — write an ADR.
- **codehealth version bumps** — pin in dev install instructions when shipping a major change. Use upstream release notes.
