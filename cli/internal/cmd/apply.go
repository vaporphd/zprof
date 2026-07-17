// cli/internal/cmd/apply.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/vaporphd/zprof/internal/apply"
	"github.com/vaporphd/zprof/internal/managed"
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
	)
	c := &cobra.Command{
		Use:   "apply <overlay> [<overlay>...]",
		Short: "Применить один или несколько overlays в текущем проекте",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo := repoDir()
			base, err := overlay.LoadBase(filepath.Join(repo, "base"))
			if err != nil {
				return fmt.Errorf("load base: %w", err)
			}
			var overlays []*overlay.Overlay
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
			pwd, _ := os.Getwd()
			if dryRun {
				fmt.Println("[dry-run] would apply overlays:", args)
				return nil
			}
			res, err := apply.Apply(apply.ApplyOpts{
				ProjectDir: pwd,
				Base:       base,
				Overlays:   overlays,
				Project:    proj,
				MergeMode:  managed.ModeOverwrite,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Создано агентов: %d\n", len(res.CreatedAgents))
			fmt.Printf("Обновлено файлов: %d\n", len(res.UpdatedFiles))
			fmt.Printf("Создано state-файлов: %d\n", len(res.StateFiles))
			return nil
		},
	}
	c.Flags().BoolVar(&minimal, "minimal", false, "Пропустить docs/PROJECT_SPEC.md и docs/adr/")
	c.Flags().BoolVar(&withGates, "with-gates", false, "Включить north-star-auditor / evidence-auditor / plan-reviewer")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "Только показать план, не писать файлы")
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
