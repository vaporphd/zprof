// cli/internal/cmd/init.go
package cmd

import (
	"os"

	"github.com/alcherk/zprof/internal/wizard"
	"github.com/spf13/cobra"
)

// NewInitCmd returns the `zprof init` command: an interactive wizard that
// detects applicable overlays, prompts for confirmation/options, and
// applies the result.
func NewInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Интерактивный wizard: detect + выбор overlays + apply",
		RunE: func(cmd *cobra.Command, args []string) error {
			pwd, _ := os.Getwd()
			return wizard.Run(wizard.Opts{ProjectDir: pwd, RepoDir: repoDir()})
		},
	}
}
