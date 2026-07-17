// cli/internal/cmd/eval.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
		format string
		since  string
		until  string
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

--format is one of "md" (default) or "html". HTML output is a
self-contained page (inline CSS, no external assets) safe to share.

--since / --until filter dispatches by the tool_use timestamp. Accepted
forms: RFC3339 ("2026-07-17T00:00:00Z"), a date ("2026-07-17"), or a
relative form ("24h", "3d"). Filtering keeps main-loop token totals
intact but restricts the per-role scorecard and violations table to
the dispatches inside the window.

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

			if since != "" || until != "" {
				sinceT, err := parseTimeArg(since)
				if err != nil {
					return fmt.Errorf("--since: %w", err)
				}
				untilT, err := parseTimeArg(until)
				if err != nil {
					return fmt.Errorf("--until: %w", err)
				}
				filtered := trace.Dispatches[:0]
				for _, d := range trace.Dispatches {
					if !sinceT.IsZero() && d.Timestamp.Before(sinceT) {
						continue
					}
					if !untilT.IsZero() && d.Timestamp.After(untilT) {
						continue
					}
					filtered = append(filtered, d)
				}
				trace.Dispatches = filtered
			}

			score := eval.Score(trace, nil)

			format = strings.ToLower(format)
			var body []byte
			var filename string
			switch format {
			case "", "md":
				body = []byte(eval.RenderSummary(score))
				filename = "summary.md"
			case "html":
				body = []byte(eval.RenderHTML(score))
				filename = "summary.html"
			default:
				return fmt.Errorf("--format must be md or html (got %q)", format)
			}

			dest := outDir
			if dest == "" {
				dest = filepath.Join(cwd, ".zprof", "eval", trace.Session.SessionID)
			}
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return fmt.Errorf("create %s: %w", dest, err)
			}
			outPath := filepath.Join(dest, filename)
			if err := os.WriteFile(outPath, body, 0o644); err != nil {
				return fmt.Errorf("write %s: %w", filename, err)
			}
			// Only spam stdout when the caller can read it — HTML is not
			// terminal-friendly, so we keep it on disk and print a pointer.
			if filename == "summary.md" {
				fmt.Fprintln(cmd.OutOrStdout(), string(body))
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "saved: %s\n", outPath)
			if deep {
				fmt.Fprintln(cmd.ErrOrStderr(), "note: --deep panel judging is not yet in-process; dispatch the `evaluator` subagent from Claude Code for now")
			}
			return nil
		},
	}
	c.Flags().StringVar(&outDir, "out", "", "Output directory (default: .zprof/eval/<sessionId>)")
	c.Flags().StringVar(&format, "format", "md", `Output format: "md" or "html"`)
	c.Flags().StringVar(&since, "since", "", "Filter dispatches after this time (RFC3339, YYYY-MM-DD, or relative like 24h/3d)")
	c.Flags().StringVar(&until, "until", "", "Filter dispatches before this time")
	c.Flags().BoolVar(&deep, "deep", false, "Also dispatch the Tier-2 evaluator subagent (placeholder)")
	return c
}

// parseTimeArg accepts three forms:
//   - RFC3339 like "2026-07-17T00:00:00Z" (canonical)
//   - date-only like "2026-07-17" (assumed UTC midnight)
//   - relative like "24h" / "3d" — subtracted from now
//
// Empty string returns a zero-time (meaning "no filter") without an error,
// so callers can always pass the flag value through unchecked.
func parseTimeArg(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	// Relative form: try duration; also accept "Nd" as "N*24h" since Go's
	// time.ParseDuration doesn't recognize days.
	if strings.HasSuffix(s, "d") {
		days := strings.TrimSuffix(s, "d")
		if d, err := time.ParseDuration(days + "h"); err == nil {
			return time.Now().Add(-24 * d), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil {
		return time.Now().Add(-d), nil
	}
	return time.Time{}, fmt.Errorf("unrecognized time format %q; try 2026-07-17, 2026-07-17T00:00:00Z, or 24h/3d", s)
}
