// cli/internal/cmd/doctor.go
package cmd

import (
	"fmt"
	"os"

	"github.com/vaporphd/zprof/internal/doctor"
	"github.com/spf13/cobra"
)

// NewDoctorCmd returns the `zprof doctor` command: diagnostics for the
// current project's .zprof.yaml, overlays, agent models, and managed
// markers.
func NewDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Диагностика текущего проекта",
		RunE: func(cmd *cobra.Command, args []string) error {
			pwd, err := os.Getwd()
			if err != nil {
				return err
			}
			issues, err := doctor.Diagnose(pwd, repoDir())
			if err != nil {
				return err
			}
			var errCount int
			for _, i := range issues {
				fmt.Printf("[%s] %s", i.Level, i.Message)
				if i.Path != "" {
					fmt.Printf(" (%s)", i.Path)
				}
				fmt.Println()
				if i.Level == doctor.LevelError {
					errCount++
				}
			}
			if len(issues) == 0 {
				fmt.Println("Проблем не найдено.")
			}
			if errCount > 0 {
				return fmt.Errorf("doctor: найдено ошибок: %d", errCount)
			}
			return nil
		},
	}
}
