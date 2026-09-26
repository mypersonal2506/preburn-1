package main

import (
	"context"
	"fmt"
	"net"

	"github.com/spf13/cobra"

	"github.com/preburn/preburn/internal/app"
)

func newServeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Serve the API and the dashboard until SIGINT or SIGTERM",
		Args:  noArguments,
		RunE: func(command *cobra.Command, _ []string) error {
			return runProcess(command, app.RoleAPI, serve)
		},
	}
}

func serve(ctx context.Context, application *app.App) error {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", application.Configuration.HTTPAddress)
	if err != nil {
		return fmt.Errorf("listen on http address: %w", err)
	}
	return application.Serve(ctx, listener)
}
