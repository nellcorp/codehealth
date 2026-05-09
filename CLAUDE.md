# CLAUDE.md

Guide for Claude Code (and humans) picking up work on this repo.

## What this is

`codehealth` is a single Go binary that exposes CodeScene + Codecov
tooling to MCP clients (stdio transport) and as a CLI. Drop it into any
repo so agents can verify code health and coverage **before** committing,
pushing, or opening a PR. Two backends, unified tool surface.

## Layout

```
.
├── cmd/codehealth/main.go     Cobra root; CLI subcommand wiring; ldflags-injected version.
├── internal/
│   ├── api/                       CodeScene REST v2 client.
│   │   ├── client.go              ProjectHealth, Hotspots, FileHealth, Components, ListCodeReviews, CodeReview, KPITrend.
│   │   ├── types.go               ProjectHealth, Hotspot, FileHealth, Biomarker, Component, CodeReview, CodeReviewFile, flexFloat.
│   │   └── client_test.go         httptest fixtures.
│   ├── codecov/                   Codecov REST v2 client.
│   │   ├── client.go              ProjectCoverage, FileCoverage, Compare.
│   │   ├── types.go               ProjectCoverage, FileCoverage, CoverageDelta.
│   │   └── client_test.go         httptest fixtures.
│   ├── config/config.go           FromEnv() + APIReady()/CoverageReady() guards.
│   ├── delta/delta.go             git-based before/after delta runner (writeBaseRev + scoring).
│   ├── local/                     local scoring backends.
│   │   ├── detect.go              Backend interface + Detect() (cs CLI vs go-fallback).
│   │   ├── cs_cli.go              shell out to `cs check --json`.
│   │   └── go_metrics.go          gocyclo + gocognit AST fallback.
│   ├── mcpsrv/server.go           MCP stdio server; tool registration in register().
│   └── thresholds/parse.go        .codescene-thresholds + .codecov-thresholds parser.
├── .goreleaser.yaml               multi-OS release config.
├── .github/workflows/             CI (vet/build/test) + release (goreleaser on tag).
└── README.md                      user-facing docs.
```

## Tool surface (12 total)

CodeScene API: `health_overview`, `list_hotspots`, `file_health`, `component_health`, `list_code_reviews`, `code_review`, `kpi_trend`.
CodeScene local: `delta_check`, `score_file`.
Codecov: `coverage_overview`, `file_coverage`, `delta_coverage`.

CodeScene API tools require `CODESCENE_TOKEN` + `CODESCENE_PROJECT_ID`.
Codecov tools require `CODECOV_TOKEN` + `CODECOV_REPO` (`service/owner/repo`).
Local CodeScene tools (`delta_check`, `score_file`) need no creds.

Each backend's `Ready()` guard is independent — partial config is fine.

### Code Review note

`code_review` reads delta-analyses **already triggered** by CodeScene's PR
integration (GitHub/GitLab/etc). The cloud REST API (`api.codescene.io`) does
not expose an on-demand `POST /delta-analysis` — that path is enterprise
self-hosted only. Flow: enable CodeScene's PR integration → CodeScene reviews
each PR → `list_code_reviews` to find the id → `code_review` for per-file
`code_health` deltas + failed gates.

## Adding a new MCP tool

In `internal/mcpsrv/server.go::register()`:

```go
s.AddTool(
    mcp.NewTool("my_tool",
        mcp.WithDescription("..."),
        mcp.WithString("path", mcp.Required(), mcp.Description("repo-relative path"))),
    func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        if err := cfg.APIReady(); err != nil {                          // or CoverageReady()
            return mcp.NewToolResultError(err.Error()), nil
        }
        path, err := req.RequireString("path")
        if err != nil { return mcp.NewToolResultError(err.Error()), nil }
        out, err := backend.Call(ctx, path)
        if err != nil { return mcp.NewToolResultError(err.Error()), nil }
        return jsonResult(out)
    },
)
```

Conventions:
- Always return `(*CallToolResult, nil)` — never propagate Go errors as the second return; surface them via `NewToolResultError`. Claude expects MCP-shaped errors.
- Marshal responses through `jsonResult` for stable JSON shape.
- Param helpers: `req.RequireString`, `req.GetString`, `req.GetBool`, `req.GetFloat`, `stringSliceArg`.

## Adding a new CLI subcommand

In `cmd/codehealth/main.go`:

1. Write a `fooCmd() *cobra.Command` factory (mirror `healthCmd`).
2. Add to `root.AddCommand(...)` in `main()`.
3. Use `RunE` (returns `error`), not `Run`. `cobra` exits 1 on error.
4. Pull config via `config.FromEnv()` then guard with the appropriate `Ready()` helper.

Warn-only commands (e.g. `delta`) must `return nil` on regression — exit 0 keeps git hooks non-blocking.

## Adding a new backend

Checklist:
1. New package under `internal/<backend>/` with `client.go` + `types.go` + `client_test.go`.
2. Hand-rolled `net/http` client w/ 20s timeout. Bearer token. Match shape of `internal/api/client.go::do()`.
3. Add fields to `internal/config/Config` + reader in `FromEnv()` + a `XReady() error` guard + sentinel `ErrXNotConfigured`.
4. Register MCP tools in `internal/mcpsrv/server.go::register()`.
5. Add CLI subcommands in `cmd/codehealth/main.go`.
6. Optional: extend `internal/thresholds/parse.go` for a floor file.
7. Update README tool table + env block. Update this file's Layout + Tool surface.

## Conventions

- **Env-only credentials.** Never accept tokens via flags.
- **20s HTTP timeout** baseline (`time.Second * 20`).
- **Each backend ready-check is independent.** A missing CodeScene token must not break Codecov tools, and vice versa.
- **MCP errors** via `NewToolResultError(err.Error())` — never propagate Go err out of the handler.
- **Local `delta_check` is warn-only.** Returns `nil` even when regressed; CI is the gate.
- **JSON responses** via `jsonResult()` (MCP) or `printJSON()` (CLI). Indented for readability.
- **Repo working dir in tests**: `t.TempDir()` + `git init -b main`. See `internal/delta/delta_test.go`.

## Build / test / release

```bash
go build ./...
go vet ./...
go test ./...
go test -race ./...                       # CI runs this
goreleaser release --snapshot --clean     # local multi-OS build
```

Tag `vX.Y.Z` to cut a release — `release.yaml` runs goreleaser.
Module path: `github.com/nellcorp/codehealth`. Binary name: `codehealth`.

## Don'ts

- **Don't rename existing tool names.** Consumer `.mcp.json` files reference them by string.
- **Don't gate one backend's tools behind another's config.** CodeScene-only and Codecov-only setups must both work.
- **Don't add deps without a clear win.** This binary is meant to ship as a single small portable file.
- **Don't add `cs` CLI as a hard requirement.** The pure-Go fallback in `internal/local/go_metrics.go` must stay functional.
- **Don't mock `git` in delta tests.** They use real `git init` in a temp dir.
