# codehealth

Portable Go binary that exposes [CodeScene](https://codescene.io) and
[Codecov](https://codecov.io) tooling to [Claude Code](https://claude.ai/code)
(and any MCP client) plus a matching CLI suitable for Git hooks and CI.

The binary speaks two protocols:

- **MCP** (`codehealth serve`) — stdio transport, registered in
  `.mcp.json`. Claude calls the tools below to read project health,
  coverage, hotspots, and validate refactors before commit.
- **CLI** (`codehealth <subcommand>`) — same backends, exit-status
  output, used by pre-commit hooks and shell scripts.

> **Migrating from `codescene-mcp` v0.1.x?** Binary + module renamed to
> `codehealth` in v0.2.0. Update `.mcp.json`'s `command` to
> `codehealth` and re-run `go install github.com/nellcorp/codehealth/cmd/codehealth@latest`.
> All existing CodeScene tool names (`health_overview`, `file_health`, …) are unchanged.

## Tools

| Tool | Source | Purpose |
|---|---|---|
| `health_overview` | CodeScene REST API | Live hotspot + average score; compared against the local `.codescene-thresholds` floor. |
| `list_hotspots` | CodeScene REST API | Top-N files by churn × complexity. |
| `file_health` | CodeScene REST API | Score + biomarkers for one repo-relative path. |
| `delta_check` | Local: `cs` CLI or gocyclo+gocognit | Scores staged or specified files vs HEAD. Use **before** committing. |
| `score_file` | Local: same as above | One-file complexity probe. |
| `coverage_overview` | Codecov REST API | Project coverage % vs the local `.codecov-thresholds` floor. |
| `file_coverage` | Codecov REST API | Per-file coverage at a branch or commit SHA. |
| `delta_coverage` | Codecov REST API | Coverage delta between two commits. Use **before** pushing/PR. |

CodeScene API tools require `CODESCENE_TOKEN` and `CODESCENE_PROJECT_ID`.
Codecov tools require `CODECOV_TOKEN` and `CODECOV_REPO`. Local CodeScene
tools work without credentials. Each backend degrades independently —
configuring only one is fine.

### Two scoring paths (CodeScene)

| Code state | Mechanism | Engine |
|---|---|---|
| Pushed and analyzed by CodeScene | REST API client (built into binary) | Authoritative — matches the CI gate. |
| Local working tree / staged | Shell out to CodeScene `cs` CLI when present; pure-Go fallback (gocyclo + gocognit) otherwise | Approximate when falling back; signals can disagree with CI. |

The CodeScene REST API only scores code that has been pushed and
analysed. Local scoring exists to give a pre-commit signal — Claude can
ask "did this change make the file healthier?" without waiting for a
post-push reanalysis. When `cs` is missing the binary prints a one-line
warning to stderr and falls back to the Go heuristic; CI remains the
authoritative gate.

Codecov has only one path — the REST API — because coverage requires an
uploaded report from CI.

## Integrating into your repo

For a complete drop-in recipe — `.mcp.json`, Lefthook hook, Makefile targets,
slash commands, CI gate, ADR template, two-phase rollout — see the
[**Integration Playbook**](docs/integration-playbook.md).

## Installation

### Pre-built binary

Download from [Releases](https://github.com/nellcorp/codehealth/releases)
or:

```bash
go install github.com/nellcorp/codehealth/cmd/codehealth@latest
```

### Optional: install CodeScene `cs` CLI

For engine-accurate local scoring, install the [CodeScene CLI](https://codescene.io/docs/guides/cli/index.html).
The binary auto-detects `cs` on PATH; without it the pure-Go fallback runs.

## Configuration

```bash
# CodeScene
export CODESCENE_TOKEN=...                          # required for CodeScene API tools
export CODESCENE_PROJECT_ID=12345                   # required for CodeScene API tools
export CODESCENE_URL=https://api.codescene.io       # default
export CS_CLI_PATH=cs                               # default

# Codecov
export CODECOV_TOKEN=...                            # required for Codecov tools
export CODECOV_REPO=github/nellcorp/codehealth      # required: service/owner/repo
export CODECOV_URL=https://api.codecov.io           # default
```

`CODECOV_REPO` is a single slug — copy from the Codecov UI URL. `service`
is one of `github`, `github_enterprise`, `gitlab`, `gitlab_enterprise`,
`bitbucket`, `bitbucket_server`.

### Threshold files

Two optional dotfiles. Missing files are treated as "no floor".

`.codescene-thresholds`:
```
HOTSPOT_THRESHOLD=9.0
AVERAGE_THRESHOLD=8.5
```

`.codecov-thresholds`:
```
COVERAGE_THRESHOLD=80.0
COVERAGE_DELTA_THRESHOLD=-0.5
```

`COVERAGE_DELTA_THRESHOLD` is the maximum allowed coverage drop in
percentage points (negative = tolerated drop, e.g. `-0.5` allows up to
0.5pp regression).

### Claude Code (`.mcp.json`)

```json
{
  "mcpServers": {
    "codehealth": {
      "command": "codehealth",
      "args": ["serve"],
      "env": {
        "CODESCENE_URL": "https://api.codescene.io",
        "CODESCENE_TOKEN": "${CODESCENE_TOKEN}",
        "CODESCENE_PROJECT_ID": "12345",
        "CODECOV_TOKEN": "${CODECOV_TOKEN}",
        "CODECOV_REPO": "github/nellcorp/codehealth"
      }
    }
  }
}
```

### Lefthook (`lefthook.yml`)

Warn-only pre-commit gate; CI remains authoritative.

```yaml
pre-commit:
  commands:
    codehealth-delta:
      glob: "*.go"
      run: |
        if ! command -v codehealth >/dev/null; then
          exit 0
        fi
        codehealth delta --staged || true
```

## CLI reference

```bash
codehealth serve                              # MCP server (stdio)
codehealth health                             # CodeScene project scores + floor
codehealth delta [--staged] [paths]           # local delta vs HEAD (warn-only)
codehealth hotspots --limit 10                # CodeScene top hotspots
codehealth file <path>                        # CodeScene file health + biomarkers
codehealth coverage                           # Codecov project coverage + floor
codehealth coverage-file <path> [--ref <r>]   # Codecov per-file coverage
codehealth coverage-delta <base> <head>       # Codecov compare base..head
```

## Development

```bash
go build ./...
go test ./...
go vet ./...
goreleaser release --snapshot --clean   # local multi-OS build
```

Releases are cut by tagging `vX.Y.Z`. The `release.yaml` workflow runs
GoReleaser and publishes archives + checksums.

## License

Apache 2.0. See [LICENSE](LICENSE).
