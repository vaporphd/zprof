package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vaporphd/zprof/internal/stats"
)

func NewEvalTelemetryCmd() *cobra.Command {
	var (
		role    string
		metric  string
		model   string
	)
	c := &cobra.Command{
		Use:   "eval-telemetry <agentlog-dir>",
		Short: "Analyze a telemetry signal and propose a contract diff",
		Long: `Reads .agentlog/ data, identifies the signal for the specified role
and metric, then dispatches the evaluator-telemetry agent to propose
a concrete text diff to the role's contract.

Example:
  zprof eval-telemetry .agentlog/ --role reviewer --metric preamble`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEvalTelemetry(cmd, args[0], role, metric, model)
		},
	}
	c.Flags().StringVar(&role, "role", "", "Role to analyze (required)")
	c.Flags().StringVar(&metric, "metric", "preamble", `Metric: "preamble", "cost", "compliance"`)
	c.Flags().StringVar(&model, "model", "sonnet", "Model for the evaluator")
	c.MarkFlagRequired("role")
	return c
}

func runEvalTelemetry(cmd *cobra.Command, agentlogDir, role, metric, model string) error {
	jsonlPath := filepath.Join(agentlogDir, "dispatches.jsonl")
	dispatches, losses, err := stats.ReadDispatches(jsonlPath)
	if err != nil {
		return fmt.Errorf("read dispatches: %w", err)
	}

	report := stats.Aggregate(dispatches, losses)

	// Find the role's health data
	var roleHealth *stats.RoleHealth
	for i := range report.Health {
		if report.Health[i].Role == role {
			roleHealth = &report.Health[i]
			break
		}
	}

	// Find role economics
	var roleEcon *stats.RoleEconomics
	for i := range report.Economics.ByRole {
		if report.Economics.ByRole[i].Role == role {
			roleEcon = &report.Economics.ByRole[i]
			break
		}
	}

	// Build signal description
	var signal strings.Builder
	signal.WriteString(fmt.Sprintf("Role: %s\n", role))
	signal.WriteString(fmt.Sprintf("Metric: %s\n", metric))
	signal.WriteString(fmt.Sprintf("Total dispatches in dataset: %d\n\n", report.TotalDispatches))

	if roleHealth != nil {
		signal.WriteString(fmt.Sprintf("Health data:\n"))
		signal.WriteString(fmt.Sprintf("  Dispatches: %d\n", roleHealth.Dispatches))
		signal.WriteString(fmt.Sprintf("  Completed: %d\n", roleHealth.Completed))
		signal.WriteString(fmt.Sprintf("  Failed: %d\n", roleHealth.Failed))
		signal.WriteString(fmt.Sprintf("  Preamble violations: %d / %d checked\n", roleHealth.PreambleCount, roleHealth.PreambleChecked))
		signal.WriteString(fmt.Sprintf("  Return parsed: %d / %d checked\n", roleHealth.ParsedCount, roleHealth.ParsedChecked))
		signal.WriteString(fmt.Sprintf("  Compliance rate: %.1f%%\n", roleHealth.ComplianceRate))
	}

	if roleEcon != nil {
		signal.WriteString(fmt.Sprintf("\nEconomics:\n"))
		signal.WriteString(fmt.Sprintf("  Total tokens: %d\n", roleEcon.Tokens.Total()))
		signal.WriteString(fmt.Sprintf("  Avg per dispatch: %d\n", roleEcon.AvgPerDispatch))
		signal.WriteString(fmt.Sprintf("  P50 duration: %dms\n", roleEcon.P50Duration))
		signal.WriteString(fmt.Sprintf("  P95 duration: %dms\n", roleEcon.P95Duration))
	}

	// Find sample dispatch IDs with violations
	var sampleIDs []string
	for _, d := range dispatches {
		if d.Role != role || !d.DispatchComplete {
			continue
		}
		switch metric {
		case "preamble":
			if d.HasPreamble != nil && *d.HasPreamble && len(sampleIDs) < 5 {
				sampleIDs = append(sampleIDs, d.DispatchID)
			}
		case "cost":
			if len(sampleIDs) < 5 {
				sampleIDs = append(sampleIDs, d.DispatchID)
			}
		case "compliance":
			if (d.HasPreamble != nil && *d.HasPreamble) || (d.ReturnParsed != nil && !*d.ReturnParsed) {
				if len(sampleIDs) < 5 {
					sampleIDs = append(sampleIDs, d.DispatchID)
				}
			}
		}
	}

	if len(sampleIDs) > 0 {
		signal.WriteString(fmt.Sprintf("\nSample dispatch IDs with violations:\n"))
		for _, id := range sampleIDs {
			signal.WriteString(fmt.Sprintf("  - %s\n", id))
		}
	}

	// Find transcript paths for samples
	signal.WriteString(fmt.Sprintf("\nTranscript directory: %s/transcripts/\n", agentlogDir))

	// Output the signal
	signalJSON, _ := json.MarshalIndent(map[string]any{
		"signal":     signal.String(),
		"role":       role,
		"metric":     metric,
		"agentlog":   agentlogDir,
		"sample_ids": sampleIDs,
	}, "", "  ")

	signalPath := filepath.Join(agentlogDir, fmt.Sprintf("signal-%s-%s.json", role, metric))
	if err := os.WriteFile(signalPath, signalJSON, 0o644); err != nil {
		return fmt.Errorf("write signal: %w", err)
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "Signal written to %s\n", signalPath)
	fmt.Fprintf(cmd.ErrOrStderr(), "Dispatching evaluator-telemetry with model=%s...\n", model)

	// Dispatch claude with the evaluator-telemetry agent
	prompt := fmt.Sprintf(`Read the telemetry signal at %s and propose a contract diff for the %s role.

The signal shows %s issues. Read the role's contract at .claude/agents/%s.md,
read 3-5 transcripts from %s/transcripts/ for the sample dispatch IDs in the signal,
and write your proposed diff to docs/reviews/.

After writing the diff file, return your verdict.`, signalPath, role, metric, role, agentlogDir)

	claudeCmd := exec.Command("claude", "-p", "--dangerously-skip-permissions", "--model", model)
	claudeCmd.Dir = filepath.Dir(agentlogDir) // project dir
	claudeCmd.Stdin = strings.NewReader(prompt)
	output, err := claudeCmd.CombinedOutput()

	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "evaluator error: %v\n", err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), string(output))
	return nil
}
