// cli/internal/cmd/eval.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/vaporphd/zprof/internal/eval"
)

// NewEvalCmd returns the `zprof eval` command. It runs Tier 1 (deterministic
// parse + score + summary render) over a Claude Code session JSONL. The
// Tier 2 flow (LLM panel judging) is triggered separately via --deep, which
// this command emits a hand-off note for and does not yet execute in-process.
func NewEvalCmd() *cobra.Command {
	var (
		outDir string
		deep   bool
	)
	c := &cobra.Command{
		Use:   "eval [<sessionId>|<path>]",
		Short: "Tier-1 deterministic scorecard for a Claude Code session log",
		Long: `Parse a session JSONL from ~/.claude/projects/<slug>/, extract every
Agent-tool dispatch, and write a per-role scorecard + contract-violation
list to <outDir>. No LLM runs at this tier. Cost: zero tokens.

With no argument the command auto-detects the most-recent log for the
current working directory. Pass a bare session ID or an absolute path
to override.

--deep is a placeholder for the Tier-2 evaluator subagent dispatch; it
is not yet in-process.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := ""
			if len(args) == 1 {
				arg = args[0]
			}
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getwd: %w", err)
			}
			path, err := eval.LocateSession(arg, cwd)
			if err != nil {
				return err
			}
			trace, err := eval.ParseSession(path)
			if err != nil {
				return err
			}
			score := eval.Score(trace, nil)
			summary := eval.RenderSummary(score)

			dest := outDir
			if dest == "" {
				dest = filepath.Join(cwd, ".zprof", "eval", trace.Session.SessionID)
			}
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return fmt.Errorf("create %s: %w", dest, err)
			}
			if err := os.WriteFile(filepath.Join(dest, "summary.md"), []byte(summary), 0o644); err != nil {
				return fmt.Errorf("write summary.md: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), summary)
			fmt.Fprintf(cmd.ErrOrStderr(), "\nsaved: %s/summary.md\n", dest)
			if deep {
				fmt.Fprintln(cmd.ErrOrStderr(), "note: --deep panel judging is not yet in-process; dispatch the `evaluator` subagent from Claude Code for now")
			}
			return nil
		},
	}
	c.Flags().StringVar(&outDir, "out", "", "Output directory (default: .zprof/eval/<sessionId>)")
	c.Flags().BoolVar(&deep, "deep", false, "Also dispatch the Tier-2 evaluator subagent (placeholder)")
	return c
}
