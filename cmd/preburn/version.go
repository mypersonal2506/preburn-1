package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/preburn/preburn/internal/version"
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version, commit and build date",
		Args:  noArguments,
		RunE: func(command *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(command.OutOrStdout(), "preburn %s (commit %s, built %s)\n",
				version.Version, version.Commit, version.BuildDate)
			return err
		},
	}
}
