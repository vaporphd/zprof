// cli/internal/cmd/list.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/alcherk/zprof/internal/manifest"
	"github.com/spf13/cobra"
)

func NewListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Что применено в текущем проекте (из .zprof.yaml)",
		RunE: func(cmd *cobra.Command, args []string) error {
			pwd, _ := os.Getwd()
			m, err := manifest.LoadProject(filepath.Join(pwd, ".zprof.yaml"))
			if err != nil {
				return fmt.Errorf("нет .zprof.yaml в текущей директории (%w)", err)
			}
			fmt.Println("Overlays:", m.Overlays)
			fmt.Println("Язык:", m.Language)
			fmt.Println("With gates:", m.WithGates)
			fmt.Println("Minimal:", m.Minimal)
			if len(m.ModelOverrides) > 0 {
				fmt.Println("Model overrides:")
				for k, v := range m.ModelOverrides {
					fmt.Printf("  %s → %s\n", k, v)
				}
			}
			if len(m.AgentOverrides) > 0 {
				fmt.Println("Agent overrides:")
				for k, v := range m.AgentOverrides {
					fmt.Printf("  %s → %s\n", k, v)
				}
			}
			return nil
		},
	}
}
