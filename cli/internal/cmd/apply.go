// cli/internal/cmd/apply.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/vaporphd/zprof/internal/apply"
	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/vaporphd/zprof/internal/overlay"
	"github.com/spf13/cobra"
)

// NewApplyCmd returns the `zprof apply <overlay> [<overlay>...]` command.
func NewApplyCmd() *cobra.Command {
	var (
		minimal   bool
		withGates bool
		dryRun    bool
		mergeFlag string
	)
	c := &cobra.Command{
		Use:   "apply <overlay> [<overlay>...]",
		Short: "Применить один или несколько overlays в текущем проекте",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, resolver, err := parseMergeFlag(mergeFlag)
			if err != nil {
				return err
			}
			repo := repoDir()
			base, err := overlay.LoadBase(filepath.Join(repo, "base"))
			if err != nil {
				return fmt.Errorf("load base: %w", err)
			}
			var overlays []*overlay.Overlay
			// base is applied implicitly and can never be an overlay arg;
			// silently strip it (with a note) rather than emitting the
			// misleading "load overlay base: manifest.yaml not found" error.
			filteredArgs := args[:0:0]
			for _, name := range args {
				if name == "base" {
					fmt.Fprintln(cmd.ErrOrStderr(), "note: \"base\" is applied implicitly; ignoring as overlay arg")
					continue
				}
				filteredArgs = append(filteredArgs, name)
			}
			args = filteredArgs
			if len(args) == 0 {
				return fmt.Errorf("no overlay names given (only \"base\", which is implicit); pass at least one overlay name")
			}
			for _, name := range args {
				o, err := overlay.LoadOverlay(filepath.Join(repo, "overlays", name))
				if err != nil {
					return fmt.Errorf("load overlay %s: %w", name, err)
				}
				overlays = append(overlays, o)
			}
			proj := &manifest.ProjectManifest{
				Overlays:  args,
				Language:  "ru",
				WithGates: withGates,
				Minimal:   minimal,
			}
			pwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getwd: %w", err)
			}
			if dryRun {
				fmt.Println("[dry-run] would apply overlays:", args)
				return nil
			}
			res, err := apply.Apply(apply.ApplyOpts{
				ProjectDir: pwd,
				Base:       base,
				Overlays:   overlays,
				Project:    proj,
				MergeMode:  mode,
				Resolver:   resolver,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Создано агентов: %d\n", len(res.CreatedAgents))
			fmt.Printf("Обновлено файлов: %d\n", len(res.UpdatedFiles))
			fmt.Printf("Создано state-файлов: %d\n", len(res.StateFiles))
			if len(res.Conflicts) > 0 {
				fmt.Printf("Конфликтов managed-блоков: %d\n", len(res.Conflicts))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&minimal, "minimal", false, "Пропустить docs/PROJECT_SPEC.md и docs/adr/")
	c.Flags().BoolVar(&withGates, "with-gates", false, "Включить north-star-auditor / evidence-auditor / plan-reviewer")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "Только показать план, не писать файлы")
	c.Flags().StringVar(&mergeFlag, "merge", "overwrite", "Стратегия конфликтов managed-блоков: overwrite | preserve | interactive")
	return c
}

// repoDir returns ~/.zprof/repo (dev fallback: ../profiles relative to CWD).
func repoDir() string {
	if p := os.Getenv("ZPROF_REPO"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".zprof", "repo")
}
