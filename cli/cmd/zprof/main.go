// cli/cmd/zprof/main.go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "0.1.0-dev"

func main() {
	root := &cobra.Command{
		Use:     "zprof",
		Short:   "Layered profile system for Claude Code",
		Version: version,
	}
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
