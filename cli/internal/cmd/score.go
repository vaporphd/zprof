// cli/internal/cmd/score.go
package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vaporphd/zprof/internal/eval"
	"github.com/vaporphd/zprof/internal/score"
	"github.com/vaporphd/zprof/internal/stats"
)

// NewScoreCmd returns `zprof score` — the per-task scorecard (spec
// docs/superpowers/specs/2026-09-26-task-scorecard-design.md §8).
func NewScoreCmd(version string) *cobra.Command {
	var (
		latest     bool
		runKey     string
		allMissing bool
		asJSON     bool
		noCollect  bool
		quiet      bool
		projectDir string
		agentlog   string
	)
	c := &cobra.Command{
		Use:   "score",
		Short: "Per-task scorecard (0–100) for the latest task-runner run",
		Long: `Reads .agentlog/dispatches.jsonl and tool-events.jsonl, groups dispatches
into task-runner runs, scores penalties P1–P7 and prints a card. Appends the
result to .agentlog/scores.jsonl and writes a ## Score section into the run
log. Deterministic — no LLM, zero tokens.

Unless --no-collect is given, the collector is flushed first so the current
session's finished dispatches are visible immediately.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			var err error
			if projectDir == "" {
				if projectDir, err = os.Getwd(); err != nil {
					return fmt.Errorf("getwd: %w", err)
				}
			}
			if agentlog == "" {
				agentlog = filepath.Join(projectDir, ".agentlog")
			}

			cfg, err := score.LoadConfig(projectDir, agentlog)
			if err != nil {
				return err
			}
			if !cfg.Enabled {
				if !quiet {
					fmt.Fprintln(out, "score disabled in .zprof.yaml (score.enabled: false)")
				}
				return nil
			}

			if !noCollect {
				if err := flushCollector(projectDir); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warn: collector flush skipped: %v\n", err)
				}
			}

			ds, _, err := stats.ReadDispatches(filepath.Join(agentlog, "dispatches.jsonl"))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("read dispatches: %w", err)
			}
			evs, err := score.ReadToolEvents(filepath.Join(agentlog, "tool-events.jsonl"))
			if err != nil {
				return err
			}
			runs := score.BuildRuns(ds, evs)

			if runKey != "" && allMissing {
				return fmt.Errorf("--run and --all-missing are mutually exclusive")
			}

			var targets []score.Card
			switch {
			case allMissing:
				keys, err := score.ReadScoredKeys(filepath.Join(agentlog, "scores.jsonl"))
				if err != nil {
					return err
				}
				hash := cfg.WeightsHash()
				legacy := 0
				for _, r := range runs {
					if keys[score.ScoreKey(score.Card{RunID: r.ID, WeightsHash: hash})] {
						continue
					}
					card := score.Compute(r, cfg, version)
					if card.Inputs.Legacy {
						legacy++
						continue
					}
					targets = append(targets, card)
				}
				if legacy > 0 && !quiet {
					fmt.Fprintf(out, "skipped %d legacy run(s) (collected before schema v2)\n", legacy)
				}
				if len(targets) == 0 {
					if !quiet {
						fmt.Fprintln(out, "nothing to score: every complete run already has a row for the current weights")
					}
					return nil
				}
			case runKey != "":
				r := score.FindRun(runs, runKey)
				if r == nil {
					return fmt.Errorf("no run matches %q (have %d runs)", runKey, len(runs))
				}
				targets = []score.Card{score.Compute(*r, cfg, version)}
			default:
				if !latest {
					return fmt.Errorf("--latest=false requires --run or --all-missing")
				}
				r := score.LatestRun(runs)
				if r == nil {
					if !quiet {
						fmt.Fprintln(out, "unscored: no task-runner dispatch in .agentlog (main dispatched directly, or run not finished)")
					}
					return nil
				}
				targets = []score.Card{score.Compute(*r, cfg, version)}
			}

			for i, card := range targets {
				if err := score.AppendScore(filepath.Join(agentlog, "scores.jsonl"), card); err != nil {
					return err
				}
				rendered := score.RenderCard(card)
				if card.RunLog != "" {
					runLogPath := card.RunLog
					if !filepath.IsAbs(runLogPath) {
						runLogPath = filepath.Join(projectDir, runLogPath)
					}
					if err := score.WriteRunLogSection(runLogPath, rendered); err != nil && !errors.Is(err, os.ErrNotExist) {
						return err
					}
				}
				if quiet {
					continue
				}
				if asJSON {
					data, _ := json.Marshal(card)
					fmt.Fprintln(out, string(data))
				} else {
					if i > 0 {
						fmt.Fprintln(out, strings.Repeat("─", 60))
					}
					fmt.Fprint(out, rendered)
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&latest, "latest", true, "Score the most recent complete run (default)")
	c.Flags().StringVar(&runKey, "run", "", "Score a specific run: run_id, run log path, or slug suffix")
	c.Flags().BoolVar(&allMissing, "all-missing", false, "Score every complete run without a row for the current weights")
	c.Flags().BoolVar(&asJSON, "json", false, "Print the scores.jsonl row instead of the card")
	c.Flags().BoolVar(&noCollect, "no-collect", false, "Do not flush the collector first")
	c.Flags().BoolVar(&quiet, "quiet", false, "Persist only, print nothing (for hooks)")
	c.Flags().StringVar(&projectDir, "project", "", "Project directory (default: cwd)")
	c.Flags().StringVar(&agentlog, "agentlog", "", "Telemetry directory (default: <project>/.agentlog)")
	return c
}

// flushCollector runs the deployed collector in `stop` mode against the most
// recent session log for projectDir, feeding it the same JSON payload the
// Claude Code hook would. Missing collector or session is reported, not fatal.
func flushCollector(projectDir string) error {
	script := filepath.Join(projectDir, ".claude", "zprof-collect.py")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("no collector at %s", script)
	}
	transcript, err := eval.LocateSession("", projectDir)
	if err != nil {
		return err
	}
	sessionID := strings.TrimSuffix(filepath.Base(transcript), ".jsonl")
	payload, _ := json.Marshal(map[string]any{
		"session_id":      sessionID,
		"transcript_path": transcript,
		"cwd":             projectDir,
	})
	cmd := exec.Command("python3", script, "stop")
	cmd.Dir = projectDir
	cmd.Stdin = bytes.NewReader(payload)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("collector: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
