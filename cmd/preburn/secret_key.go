package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/preburn/preburn/internal/secrets"
)

func newSecretKeyCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "secret-key",
		Short: "Print a new random value for PREBURN_SECRET_KEY",
		Args:  noArguments,
		RunE: func(command *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(command.OutOrStdout(), secrets.NewSecretKey())
			return err
		},
	}
}
