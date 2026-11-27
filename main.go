package main

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:           "api-bin",
	Short:         "Generate binary readers from a declarative XML spec",
	SilenceUsage:  true,
	SilenceErrors: true,
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		rootCmd.PrintErrln("api-bin:", err)
		os.Exit(1)
	}
}
