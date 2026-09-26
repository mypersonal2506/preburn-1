package main

import (
	"github.com/spf13/cobra"

	"github.com/preburn/preburn/internal/app"
)

func newOpenAPICommand() *cobra.Command {
	return &cobra.Command{
		Use:   "openapi",
		Short: "Write the OpenAPI document to standard output",
		Args:  noArguments,
		RunE: func(command *cobra.Command, _ []string) error {
			return app.WriteOpenAPI(command.OutOrStdout())
		},
	}
}
