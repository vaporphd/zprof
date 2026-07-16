// cli/internal/cmd/models.go
package cmd

import (
	"fmt"
	"sort"

	"github.com/alcherk/zprof/internal/models"
	"github.com/spf13/cobra"
)

// NewModelsCmd returns the `zprof models` command group.
func NewModelsCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "models",
		Short: "Работа с реестром моделей",
	}
	root.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "Показать таблицу tier alias → exact ID",
		RunE: func(cmd *cobra.Command, args []string) error {
			keys := make([]string, 0, len(models.Aliases))
			for k := range models.Aliases {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Printf("%-8s  %s\n", k, models.Aliases[k])
			}
			return nil
		},
	})
	return root
}
