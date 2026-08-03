package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/vaporphd/zprof/internal/stats"
)

func NewStatsCmd() *cobra.Command {
	var (
		outPath   string
		format    string
		sessionID string
		role      string
	)
	c := &cobra.Command{
		Use:   "stats <agentlog-dir> [<agentlog-dir>...]",
		Short: "Generate telemetry dashboard from .agentlog/ data",
		Long: `Reads dispatches.jsonl from each .agentlog/ directory and produces
a decision-oriented HTML dashboard with role health, economics,
routes, and profile drift reports.

Use --session to filter to a single session (replaces zprof eval).
Use --role to filter to a single role.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, dir := range args {
				jsonlPath := filepath.Join(dir, "dispatches.jsonl")
				dispatches, losses, err := stats.ReadDispatches(jsonlPath)
				if err != nil {
					return fmt.Errorf("read %s: %w", jsonlPath, err)
				}

				if sessionID != "" {
					var filtered []stats.Dispatch
					for _, d := range dispatches {
						if d.SessionID == sessionID {
							filtered = append(filtered, d)
						}
					}
					dispatches = filtered
				}
				if role != "" {
					var filtered []stats.Dispatch
					for _, d := range dispatches {
						if d.Role == role {
							filtered = append(filtered, d)
						}
					}
					dispatches = filtered
				}

				report := stats.Aggregate(dispatches, losses)

				absDir, _ := filepath.Abs(dir)
				projectDir := filepath.Dir(absDir)
				report.ProjectName = filepath.Base(projectDir)

				var output []byte
				var ext string
				switch format {
				case "json":
					output, err = json.MarshalIndent(report, "", "  ")
					if err != nil {
						return fmt.Errorf("marshal report: %w", err)
					}
					output = append(output, '\n')
					ext = ".json"
				default:
					output = []byte(stats.RenderHTML(report))
					ext = ".html"
				}

				dest := outPath
				if dest == "" {
					dest = filepath.Join(dir, "report"+ext)
				}

				if err := os.WriteFile(dest, output, 0o644); err != nil {
					return fmt.Errorf("write %s: %w", dest, err)
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "saved: %s (%d dispatches, %d sessions)\n", dest, report.TotalDispatches, report.Sessions)
			}
			return nil
		},
	}
	c.Flags().StringVar(&outPath, "out", "", "Output path (default: <agentlog-dir>/report.{html,json})")
	c.Flags().StringVar(&format, "format", "html", `Output format: "html" or "json"`)
	c.Flags().StringVar(&sessionID, "session", "", "Filter to a single session ID")
	c.Flags().StringVar(&role, "role", "", "Filter to a single role")
	return c
}
