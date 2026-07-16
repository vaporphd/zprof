// cli/internal/cmd/sync.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/alcherk/zprof/internal/apply"
	"github.com/alcherk/zprof/internal/managed"
	"github.com/alcherk/zprof/internal/manifest"
	"github.com/alcherk/zprof/internal/overlay"
	sy "github.com/alcherk/zprof/internal/sync"
	"github.com/spf13/cobra"
)

const defaultRemote = "https://github.com/alcherk/zprof-profiles.git"

// NewSyncCmd returns the `zprof sync` command.
func NewSyncCmd() *cobra.Command {
	var remote string
	c := &cobra.Command{
		Use:   "sync",
		Short: "Обновить локальный репозиторий профилей + перегенерировать managed-блоки",
		RunE: func(cmd *cobra.Command, args []string) error {
			repo := repoDir()
			if err := sy.EnsureRepo(remote, repo); err != nil {
				return fmt.Errorf("sync repo: %w", err)
			}
			fmt.Printf("Репозиторий обновлён: %s\n", repo)

			// Re-apply if .zprof.yaml present in current project.
			pwd, _ := os.Getwd()
			mfPath := filepath.Join(pwd, ".zprof.yaml")
			if _, err := os.Stat(mfPath); err != nil {
				fmt.Println("В текущей директории нет .zprof.yaml — sync только обновил репо.")
				return nil
			}
			proj, err := manifest.LoadProject(mfPath)
			if err != nil {
				return err
			}
			base, err := overlay.LoadBase(filepath.Join(repo, "base"))
			if err != nil {
				return err
			}
			var overlays []*overlay.Overlay
			for _, name := range proj.Overlays {
				o, err := overlay.LoadOverlay(filepath.Join(repo, "overlays", name))
				if err != nil {
					return err
				}
				overlays = append(overlays, o)
			}
			_, err = apply.Apply(apply.ApplyOpts{
				ProjectDir: pwd, Base: base, Overlays: overlays,
				Project: proj, MergeMode: managed.ModeOverwrite,
			})
			if err != nil {
				return err
			}
			fmt.Println("Managed-блоки перегенерированы.")
			return nil
		},
	}
	c.Flags().StringVar(&remote, "remote", defaultRemote, "URL репозитория профилей")
	return c
}
