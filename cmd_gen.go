// cmd_gen.go holds generateFile.

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/wow-look-at-my/api-bin/binspec"
)

func init() {
	var out string
	cmd := &cobra.Command{
		Use:   "gen <spec.xml>",
		Short: "Generate the Go readers a spec describes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, err := generateFile(args[0])
			if err != nil {
				return err
			}
			if out == "" {
				_, err = cmd.OutOrStdout().Write(src)
				return err
			}
			return os.WriteFile(out, src, 0o644)
		},
	}
	cmd.Flags().StringVarP(&out, "out", "o", "", "write the generated file here instead of stdout")
	rootCmd.AddCommand(cmd)
}

// generateFile reads one spec and returns its generated Go source.
func generateFile(specPath string) ([]byte, error) {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return nil, err
	}
	spec, err := binspec.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", specPath, err)
	}
	return binspec.Generate(spec, specPath)
}
