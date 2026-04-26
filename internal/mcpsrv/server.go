// Package mcpsrv exposes the CodeScene tools over MCP (stdio transport).
package mcpsrv

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/nellcorp/codescene-mcp/internal/api"
	"github.com/nellcorp/codescene-mcp/internal/config"
	"github.com/nellcorp/codescene-mcp/internal/delta"
	"github.com/nellcorp/codescene-mcp/internal/local"
	"github.com/nellcorp/codescene-mcp/internal/thresholds"
)

// Version is reported in the MCP handshake. Overwritten at link time
// (-ldflags "-X .../mcpsrv.Version=v0.1.0").
var Version = "dev"

// Run starts the stdio MCP server and blocks until the transport closes.
func Run(ctx context.Context, cfg *config.Config) error {
	s := server.NewMCPServer("codescene", Version)
	register(s, cfg)
	return server.ServeStdio(s)
}

func register(s *server.MCPServer, cfg *config.Config) {
	apiClient := api.New(cfg.APIBaseURL, cfg.APIToken, cfg.ProjectID)
	backend := local.Detect(cfg.CSCLIPath)

	s.AddTool(
		mcp.NewTool("health_overview",
			mcp.WithDescription("Project-level CodeScene scores plus the local repository's threshold floor. Use this first to know whether the codebase is currently passing the quality gate.")),
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := cfg.APIReady(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			ph, err := apiClient.ProjectHealth(ctx)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			th, _ := thresholds.Load(".codescene-thresholds")
			return jsonResult(map[string]any{
				"hotspot":           ph.Hotspot,
				"average":           ph.Average,
				"hotspot_threshold": th.Hotspot,
				"average_threshold": th.Average,
				"passing":           ph.Hotspot >= th.Hotspot && ph.Average >= th.Average,
			})
		},
	)

	s.AddTool(
		mcp.NewTool("list_hotspots",
			mcp.WithDescription("Top-N hotspots ranked by churn × complexity. Highest-priority refactor targets."),
			mcp.WithNumber("limit", mcp.Description("number of hotspots to return"), mcp.DefaultNumber(10))),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := cfg.APIReady(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			limit := int(req.GetFloat("limit", 10))
			hs, err := apiClient.Hotspots(ctx, limit)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonResult(hs)
		},
	)

	s.AddTool(
		mcp.NewTool("file_health",
			mcp.WithDescription("CodeScene health score and biomarkers for a specific file. Read the file before/after refactor to verify improvement."),
			mcp.WithString("path", mcp.Required(), mcp.Description("repo-relative path"))),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if err := cfg.APIReady(); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			path, err := req.RequireString("path")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			fh, err := apiClient.FileHealth(ctx, path)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonResult(fh)
		},
	)

	s.AddTool(
		mcp.NewTool("delta_check",
			mcp.WithDescription("Score staged or specified files locally against a base revision (default HEAD). Returns per-file deltas. Use BEFORE committing to confirm changes are net-positive."),
			mcp.WithBoolean("staged", mcp.Description("pull paths from `git diff --cached`"), mcp.DefaultBool(false)),
			mcp.WithArray("paths", mcp.Description("explicit list of repo-relative paths"))),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			staged := req.GetBool("staged", false)
			paths := stringSliceArg(req, "paths")
			res, err := delta.Run(ctx, delta.Options{
				Staged:  staged,
				Paths:   paths,
				Backend: backend,
			})
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonResult(res)
		},
	)

	s.AddTool(
		mcp.NewTool("score_file",
			mcp.WithDescription("Local complexity score for one working-tree file. Faster than delta_check when the goal is just 'how complex is this file right now'."),
			mcp.WithString("path", mcp.Required(), mcp.Description("absolute or repo-relative path"))),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			path, err := req.RequireString("path")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			score, err := backend.Score(ctx, path)
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return jsonResult(score)
		},
	)
}

func jsonResult(v any) (*mcp.CallToolResult, error) {
	buf, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("marshal: %v", err)), nil
	}
	return mcp.NewToolResultText(string(buf)), nil
}

func stringSliceArg(req mcp.CallToolRequest, key string) []string {
	raw := req.GetArguments()
	v, ok := raw[key]
	if !ok || v == nil {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
