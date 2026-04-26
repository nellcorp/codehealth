// Command codescene-mcp exposes CodeScene tooling over MCP and as a CLI.
//
// Usage:
//
//	codescene-mcp serve              # stdio MCP server
//	codescene-mcp health             # project scores + threshold floor
//	codescene-mcp delta [--staged]   # local delta check
//	codescene-mcp hotspots [--limit] # top hotspots
//	codescene-mcp file <path>        # file health + biomarkers
//
// All commands read configuration from environment variables. See README.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/nellcorp/codescene-mcp/internal/api"
	"github.com/nellcorp/codescene-mcp/internal/config"
	"github.com/nellcorp/codescene-mcp/internal/delta"
	"github.com/nellcorp/codescene-mcp/internal/local"
	"github.com/nellcorp/codescene-mcp/internal/mcpsrv"
	"github.com/nellcorp/codescene-mcp/internal/thresholds"
)

// version is overridden at build time via -ldflags.
var version = "dev"

func main() {
	local.WarnFallbackTo = func(msg string) { fmt.Fprintln(os.Stderr, msg) }

	root := &cobra.Command{
		Use:           "codescene-mcp",
		Short:         "CodeScene MCP server + CLI",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(serveCmd(), healthCmd(), deltaCmd(), hotspotsCmd(), fileCmd())

	if err := root.ExecuteContext(rootCtx()); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func rootCtx() context.Context {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	_ = cancel // released on process exit
	return ctx
}

func serveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run the MCP server over stdio",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := config.FromEnv()
			mcpsrv.Version = version
			return mcpsrv.Run(cmd.Context(), cfg)
		},
	}
}

func healthCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Print project-level scores and the local threshold floor",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := config.FromEnv()
			if err := cfg.APIReady(); err != nil {
				return err
			}
			ph, err := api.New(cfg.APIBaseURL, cfg.APIToken, cfg.ProjectID).ProjectHealth(cmd.Context())
			if err != nil {
				return err
			}
			th, _ := thresholds.Load(".codescene-thresholds")
			fmt.Printf("hotspot: %.2f  (floor %.2f)\n", ph.Hotspot, th.Hotspot)
			fmt.Printf("average: %.2f  (floor %.2f)\n", ph.Average, th.Average)
			if th.Hotspot > 0 && ph.Hotspot < th.Hotspot {
				fmt.Println("WARNING: hotspot below floor")
			}
			if th.Average > 0 && ph.Average < th.Average {
				fmt.Println("WARNING: average below floor")
			}
			return nil
		},
	}
}

func deltaCmd() *cobra.Command {
	var staged bool
	c := &cobra.Command{
		Use:   "delta [paths...]",
		Short: "Score staged or specified files vs HEAD (warn-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.FromEnv()
			res, err := delta.Run(cmd.Context(), delta.Options{
				Paths:   args,
				Staged:  staged,
				Backend: local.Detect(cfg.CSCLIPath),
			})
			if err != nil {
				return err
			}
			if len(res.Files) == 0 {
				fmt.Println("delta: no scorable files")
				return nil
			}
			fmt.Printf("delta backend=%s net=%+0.2f\n", res.Backend, res.NetDelta)
			for _, d := range res.Files {
				flag := "ok"
				if d.Delta < 0 {
					flag = "REGRESSION"
				}
				fmt.Printf("  %s: %.2f -> %.2f (%+0.2f) [%s]\n", d.Path, d.Before, d.After, d.Delta, flag)
				for _, s := range d.SmellsAdded {
					fmt.Printf("    %s:%d %s (%s)\n", s.Function, s.Line, s.Kind, s.Message)
				}
			}
			if res.Regressed {
				fmt.Println("delta: net-negative — consider refactor before commit (warn-only).")
			}
			return nil // never non-zero — hook is warn-only
		},
	}
	c.Flags().BoolVar(&staged, "staged", false, "pull paths from `git diff --cached`")
	return c
}

func hotspotsCmd() *cobra.Command {
	var limit int
	c := &cobra.Command{
		Use:   "hotspots",
		Short: "Print top hotspots from CodeScene",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := config.FromEnv()
			if err := cfg.APIReady(); err != nil {
				return err
			}
			hs, err := api.New(cfg.APIBaseURL, cfg.APIToken, cfg.ProjectID).Hotspots(cmd.Context(), limit)
			if err != nil {
				return err
			}
			return printJSON(hs)
		},
	}
	c.Flags().IntVar(&limit, "limit", 10, "number of hotspots to return")
	return c
}

func fileCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "file <path>",
		Short: "Print CodeScene health + biomarkers for one file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.FromEnv()
			if err := cfg.APIReady(); err != nil {
				return err
			}
			fh, err := api.New(cfg.APIBaseURL, cfg.APIToken, cfg.ProjectID).FileHealth(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printJSON(fh)
		},
	}
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
