package main

import (
	"github.com/spf13/cobra"

	"github.com/preburn/preburn/internal/app"
	"github.com/preburn/preburn/internal/logging"
)

func newMigrateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply the pending database migrations, import the pricing catalog and exit",
		Args:  noArguments,
		RunE: func(command *cobra.Command, _ []string) error {
			configuration, err := loadConfiguration()
			if err != nil {
				return err
			}
			return app.Migrate(command.Context(), configuration, logging.New(command.OutOrStdout(), configuration.LogLevel))
		},
	}
}
