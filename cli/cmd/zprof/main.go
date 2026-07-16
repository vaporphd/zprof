// cli/cmd/zprof/main.go
package main

import (
	"fmt"
	"os"

	"github.com/alcherk/zprof/internal/cmd"
	"github.com/spf13/cobra"
)

var version = "0.1.0-dev"

func main() {
	root := &cobra.Command{
		Use:     "zprof",
		Short:   "Layered profile system for Claude Code",
		Version: version,
	}
	root.AddCommand(cmd.NewModelsCmd())
	root.AddCommand(cmd.NewApplyCmd())
	root.AddCommand(cmd.NewSyncCmd())
	root.AddCommand(cmd.NewInitCmd())
	root.AddCommand(cmd.NewListCmd())
	root.AddCommand(cmd.NewAgentsCmd())
	root.AddCommand(cmd.NewDoctorCmd())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
