// cli/internal/cmd/agents.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/vaporphd/zprof/internal/manifest"
	"github.com/vaporphd/zprof/internal/models"
	"github.com/spf13/cobra"
)

// NewAgentsCmd returns the `zprof agents` command group.
func NewAgentsCmd() *cobra.Command {
	root := &cobra.Command{Use: "agents", Short: "Управление агентами и моделями"}
	var modelFlag string
	set := &cobra.Command{
		Use:   "set <role> [<agent-name>]",
		Short: "Свап агента или override модели для роли",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			pwd, _ := os.Getwd()
			mfPath := filepath.Join(pwd, ".zprof.yaml")
			m, err := manifest.LoadProject(mfPath)
			if err != nil {
				return fmt.Errorf("нет .zprof.yaml в текущей директории (%w)", err)
			}
			role := args[0]
			if len(args) == 2 {
				if m.AgentOverrides == nil {
					m.AgentOverrides = map[string]string{}
				}
				m.AgentOverrides[role] = args[1]
			}
			if modelFlag != "" {
				if _, err := models.Resolve(modelFlag); err != nil {
					return err
				}
				if m.ModelOverrides == nil {
					m.ModelOverrides = map[string]string{}
				}
				m.ModelOverrides[role] = modelFlag
			}
			if err := m.Save(mfPath); err != nil {
				return err
			}
			fmt.Printf("Обновлено: %s\n", role)
			return nil
		},
	}
	set.Flags().StringVar(&modelFlag, "model", "", "Alias (opus/sonnet/haiku/opus-1m/fable) или exact claude-* ID")
	root.AddCommand(set)
	return root
}
