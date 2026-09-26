package main

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/preburn/preburn/internal/app"
)

func newWorkerCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "worker",
		Short: "Work background jobs until SIGINT or SIGTERM",
		Args:  noArguments,
		RunE: func(command *cobra.Command, _ []string) error {
			return runProcess(command, app.RoleWorker, func(ctx context.Context, application *app.App) error {
				return application.Work(ctx)
			})
		},
	}
}
