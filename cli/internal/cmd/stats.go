package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vaporphd/zprof/internal/score"
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

				guardEvs, err := score.ReadGuardEvents(filepath.Join(dir, "guard-events.jsonl"))
				if err != nil {
					return fmt.Errorf("read guard events: %w", err)
				}
				if line := guardTopRules(guardEvs, sessionID, role); line != "" {
					fmt.Fprintln(cmd.ErrOrStderr(), line)
				}
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

// guardTopRules counts guard-events.jsonl deny/block rows by rule — the same
// "what counts as a violation" filter as computeP7 (ADR-0008 H3/H6) — after
// applying the same --session/--role filters already applied to dispatches
// above. Returns "" when nothing was counted (missing file or every row
// filtered out); otherwise one line, top 5 rules, count desc then rule asc.
func guardTopRules(events []score.GuardEvent, sessionID, role string) string {
	counts := map[string]int{}
	for _, e := range events {
		if e.Decision != "deny" && e.Decision != "block" {
			continue
		}
		if sessionID != "" && e.SessionID != sessionID {
			continue
		}
		if role != "" && e.Role != role {
			continue
		}
		counts[e.Rule]++
	}
	if len(counts) == 0 {
		return ""
	}
	type kv struct {
		rule  string
		count int
	}
	items := make([]kv, 0, len(counts))
	for r, n := range counts {
		items = append(items, kv{r, n})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count != items[j].count {
			return items[i].count > items[j].count
		}
		return items[i].rule < items[j].rule
	})
	if len(items) > 5 {
		items = items[:5]
	}
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, fmt.Sprintf("%s×%d", it.rule, it.count))
	}
	return "guard: top rules: " + strings.Join(parts, " ")
}
