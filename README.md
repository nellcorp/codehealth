# codescene-mcp

Portable Go binary that exposes [CodeScene](https://codescene.io) tooling
to [Claude Code](https://claude.ai/code) (and any MCP client) plus a
matching CLI suitable for Git hooks and CI.

The binary speaks two protocols:

- **MCP** (`codescene-mcp serve`) — stdio transport, registered in
  `.mcp.json`. Claude calls the tools below to read project health,
  inspect hotspots, and validate refactors before commit.
- **CLI** (`codescene-mcp <subcommand>`) — same backends, exit-status
  output, used by pre-commit hooks and shell scripts.

## Tools

| Tool | Source | Purpose |
|---|---|---|
| `health_overview` | CodeScene REST API | Live hotspot + average score; compared against the local `.codescene-thresholds` floor. |
| `list_hotspots` | CodeScene REST API | Top-N files by churn × complexity. |
| `file_health` | CodeScene REST API | Score + biomarkers for one repo-relative path. |
| `delta_check` | Local: `cs` CLI or gocyclo+gocognit | Scores staged or specified files vs HEAD. Use **before** committing. |
| `score_file` | Local: same as above | One-file complexity probe. |

API tools require `CODESCENE_TOKEN` and `CODESCENE_PROJECT_ID`; local
tools work without credentials.

### Two scoring paths

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

## Installation

### Pre-built binary

Download from [Releases](https://github.com/nellcorp/codescene-mcp/releases)
or:

```bash
go install github.com/nellcorp/codescene-mcp/cmd/codescene-mcp@latest
```

### Optional: install CodeScene `cs` CLI

For engine-accurate local scoring, install the [CodeScene CLI](https://codescene.io/docs/guides/cli/index.html).
The binary auto-detects `cs` on PATH; without it the pure-Go fallback runs.

## Configuration

```bash
export CODESCENE_TOKEN=...            # required for API tools
export CODESCENE_PROJECT_ID=12345     # required for API tools
export CODESCENE_URL=https://api.codescene.io   # default
export CS_CLI_PATH=cs                 # default
```

### Claude Code (`.mcp.json`)

```json
{
  "mcpServers": {
    "codescene": {
      "command": "codescene-mcp",
      "args": ["serve"],
      "env": {
        "CODESCENE_URL": "https://api.codescene.io",
        "CODESCENE_TOKEN": "${CODESCENE_TOKEN}",
        "CODESCENE_PROJECT_ID": "12345"
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
    codescene-delta:
      glob: "*.go"
      run: |
        if ! command -v codescene-mcp >/dev/null; then
          exit 0
        fi
        codescene-mcp delta --staged || true
```

## CLI reference

```bash
codescene-mcp serve                    # MCP server (stdio)
codescene-mcp health                   # project scores + threshold floor
codescene-mcp delta [--staged] [paths] # local delta vs HEAD (warn-only)
codescene-mcp hotspots --limit 10      # top hotspots
codescene-mcp file <path>              # file health + biomarkers
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
