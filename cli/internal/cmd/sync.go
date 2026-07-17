// cli/internal/cmd/sync.go
package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/vaporphd/zprof/internal/apply"
	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/vaporphd/zprof/internal/overlay"
	sy "github.com/vaporphd/zprof/internal/sync"
	"github.com/spf13/cobra"
)

const defaultRemote = "https://github.com/vaporphd/zprof-profiles.git"

// syncTimeout bounds the git-fetch so a stalled remote can't hang the CLI
// forever without a SIGKILL. Overridable via --timeout for slow networks or
// unusually large profile repos.
const defaultSyncTimeout = 60 * time.Second

// NewSyncCmd returns the `zprof sync` command.
func NewSyncCmd() *cobra.Command {
	var (
		remote    string
		mergeFlag string
		timeout   time.Duration
	)
	c := &cobra.Command{
		Use:   "sync",
		Short: "Обновить локальный репозиторий профилей + перегенерировать managed-блоки",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, resolver, err := parseMergeFlag(mergeFlag)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			repo := repoDir()
			if err := sy.EnsureRepoCtx(ctx, remote, repo); err != nil {
				return fmt.Errorf("sync repo: %w", err)
			}
			fmt.Printf("Репозиторий обновлён: %s\n", repo)

			// Re-apply if .zprof.yaml present in current project.
			pwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getwd: %w", err)
			}
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
			res, err := apply.Apply(apply.ApplyOpts{
				ProjectDir: pwd, Base: base, Overlays: overlays,
				Project: proj, MergeMode: mode, Resolver: resolver,
			})
			if err != nil {
				return err
			}
			fmt.Println("Managed-блоки перегенерированы.")
			if len(res.Conflicts) > 0 {
				fmt.Printf("Конфликтов managed-блоков: %d\n", len(res.Conflicts))
			}
			return nil
		},
	}
	c.Flags().StringVar(&remote, "remote", defaultRemote, "URL репозитория профилей")
	c.Flags().StringVar(&mergeFlag, "merge", "overwrite", "Стратегия конфликтов managed-блоков: overwrite | preserve | interactive")
	c.Flags().DurationVar(&timeout, "timeout", defaultSyncTimeout, "Таймаут git-sync")
	return c
}
