// Command codehealth exposes CodeScene + Codecov tooling over MCP and as a CLI.
//
// Usage:
//
//	codehealth serve                              # stdio MCP server
//	codehealth health                             # CodeScene project scores + floor
//	codehealth delta [--staged]                   # local delta check
//	codehealth hotspots [--limit]                 # CodeScene top hotspots
//	codehealth file <path>                        # CodeScene file health + biomarkers
//	codehealth list-code-reviews [--page]         # CodeScene Code Reviews (delta-analyses) list
//	codehealth code-review <id>                   # one Code Review by id
//	codehealth components                         # architectural-component health
//	codehealth kpi-trend <factor> [kpi] [--start] # 4-factors dashboard trend line
//	codehealth coverage                           # Codecov project coverage + floor
//	codehealth coverage-file <path> [--ref <r>]   # Codecov per-file coverage
//	codehealth coverage-delta <base> <head>       # Codecov compare base..head
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

	"github.com/nellcorp/codehealth/internal/api"
	"github.com/nellcorp/codehealth/internal/codecov"
	"github.com/nellcorp/codehealth/internal/config"
	"github.com/nellcorp/codehealth/internal/delta"
	"github.com/nellcorp/codehealth/internal/local"
	"github.com/nellcorp/codehealth/internal/mcpsrv"
	"github.com/nellcorp/codehealth/internal/thresholds"
)

// version is overridden at build time via -ldflags.
var version = "dev"

func main() {
	local.WarnFallbackTo = func(msg string) { fmt.Fprintln(os.Stderr, msg) }
	local.WarnUnparsableCSTo = func(msg string) { fmt.Fprintln(os.Stderr, msg) }

	root := &cobra.Command{
		Use:           "codehealth",
		Short:         "CodeScene + Codecov MCP server + CLI",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		serveCmd(),
		healthCmd(), deltaCmd(), hotspotsCmd(), fileCmd(),
		listCodeReviewsCmd(), codeReviewCmd(), componentsCmd(), kpiTrendCmd(),
		coverageCmd(), coverageFileCmd(), coverageDeltaCmd(),
	)

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
		Short: "Print CodeScene project scores + threshold floor",
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
	var (
		staged bool
		strict bool
	)
	c := &cobra.Command{
		Use:   "delta [paths...]",
		Short: "Score staged or specified files vs HEAD (warn-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.FromEnv()
			var backend local.Backend
			if strict {
				b, err := local.DetectStrict(cfg.CSCLIPath)
				if err != nil {
					return err
				}
				backend = b
			} else {
				backend = local.Detect(cfg.CSCLIPath)
			}
			res, err := delta.Run(cmd.Context(), delta.Options{
				Paths:   args,
				Staged:  staged,
				Backend: backend,
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
	c.Flags().BoolVar(&strict, "strict", false, "fail (exit non-zero) when the CodeScene `cs` CLI is not on PATH instead of falling back to the gocyclo+gocognit engine")
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

func listCodeReviewsCmd() *cobra.Command {
	var (
		page   int
		filter string
	)
	c := &cobra.Command{
		Use:   "list-code-reviews",
		Short: "List CodeScene Code Reviews (delta-analyses) for the project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := config.FromEnv()
			if err := cfg.APIReady(); err != nil {
				return err
			}
			out, err := api.New(cfg.APIBaseURL, cfg.APIToken, cfg.ProjectID).
				ListCodeReviews(cmd.Context(), page, filter)
			if err != nil {
				return err
			}
			return printJSON(out)
		},
	}
	c.Flags().IntVar(&page, "page", 1, "1-based page number")
	c.Flags().StringVar(&filter, "filter", "", "CodeScene filter expression")
	return c
}

func codeReviewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "code-review <id>",
		Short: "Print one CodeScene Code Review (delta-analysis) by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.FromEnv()
			if err := cfg.APIReady(); err != nil {
				return err
			}
			out, err := api.New(cfg.APIBaseURL, cfg.APIToken, cfg.ProjectID).
				CodeReview(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return printJSON(out)
		},
	}
}

func componentsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "components",
		Short: "Print architectural-component health from the latest analysis",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := config.FromEnv()
			if err := cfg.APIReady(); err != nil {
				return err
			}
			comps, err := api.New(cfg.APIBaseURL, cfg.APIToken, cfg.ProjectID).
				Components(cmd.Context())
			if err != nil {
				return err
			}
			return printJSON(comps)
		},
	}
}

func kpiTrendCmd() *cobra.Command {
	var start, end string
	c := &cobra.Command{
		Use:   "kpi-trend <factor> [kpi]",
		Short: "Print a CodeScene 4-factors trend line",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.FromEnv()
			if err := cfg.APIReady(); err != nil {
				return err
			}
			factor := args[0]
			if !api.IsValidKPIFactor(factor) {
				return fmt.Errorf("factor must be one of %v", api.ValidKPIFactors)
			}
			var kpi string
			if len(args) > 1 {
				kpi = args[1]
			}
			raw, err := api.New(cfg.APIBaseURL, cfg.APIToken, cfg.ProjectID).
				KPITrend(cmd.Context(), factor, kpi, start, end)
			if err != nil {
				return err
			}
			fmt.Println(string(raw))
			return nil
		},
	}
	c.Flags().StringVar(&start, "start", "", "ISO-8601 start date (YYYY-MM-DD)")
	c.Flags().StringVar(&end, "end", "", "ISO-8601 end date (YYYY-MM-DD)")
	return c
}

func coverageCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "coverage",
		Short: "Print Codecov project coverage + threshold floor",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := config.FromEnv()
			if err := cfg.CoverageReady(); err != nil {
				return err
			}
			cov, err := codecov.New(cfg.CodecovBaseURL, cfg.CodecovToken, cfg.CodecovSlug).
				ProjectCoverage(cmd.Context())
			if err != nil {
				return err
			}
			th, _ := thresholds.Load(".codecov-thresholds")
			fmt.Printf("coverage: %.2f%%  (floor %.2f%%)\n", cov.Coverage, th.Coverage)
			if th.Coverage > 0 && cov.Coverage < th.Coverage {
				fmt.Println("WARNING: coverage below floor")
			}
			return nil
		},
	}
}

func coverageFileCmd() *cobra.Command {
	var ref string
	c := &cobra.Command{
		Use:   "coverage-file <path>",
		Short: "Print Codecov per-file coverage",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.FromEnv()
			if err := cfg.CoverageReady(); err != nil {
				return err
			}
			fc, err := codecov.New(cfg.CodecovBaseURL, cfg.CodecovToken, cfg.CodecovSlug).
				FileCoverage(cmd.Context(), ref, args[0])
			if err != nil {
				return err
			}
			return printJSON(fc)
		},
	}
	c.Flags().StringVar(&ref, "ref", "", "branch name or commit SHA (default: repo default branch)")
	return c
}

func coverageDeltaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "coverage-delta <base> <head>",
		Short: "Print Codecov coverage delta between two commits",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.FromEnv()
			if err := cfg.CoverageReady(); err != nil {
				return err
			}
			cd, err := codecov.New(cfg.CodecovBaseURL, cfg.CodecovToken, cfg.CodecovSlug).
				Compare(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			th, _ := thresholds.Load(".codecov-thresholds")
			fmt.Printf("base:  %.2f%%\n", cd.BaseCoverage)
			fmt.Printf("head:  %.2f%%\n", cd.HeadCoverage)
			fmt.Printf("delta: %+.2f%%  (floor %+.2f%%)\n", cd.Delta, th.CoverageDelta)
			if th.CoverageDelta != 0 && cd.Delta < th.CoverageDelta {
				fmt.Println("WARNING: coverage delta below floor")
			}
			return nil
		},
	}
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
