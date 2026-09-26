package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/preburn/preburn/internal/apikeys"
	"github.com/preburn/preburn/internal/app"
	"github.com/preburn/preburn/internal/httpapi"
)

const (
	environmentFlag = "environment"
	scopeFlag       = "scope"
)

func newAdminAPIKeyCommand() *cobra.Command {
	return newGroupCommand("api-key", "Create API keys", newAdminAPIKeyCreateCommand())
}

func newAdminAPIKeyCreateCommand() *cobra.Command {
	var environment, scope, name string
	create := &cobra.Command{
		Use:   "create",
		Short: "Create an API key and print only its secret",
		Args:  noArguments,
		RunE: func(command *cobra.Command, _ []string) error {
			err := errors.Join(requiredFlag(environmentFlag, environment), requiredFlag(scopeFlag, scope), requiredFlag(nameFlag, name))
			if err != nil {
				return err
			}
			keyEnvironment, err := httpapi.ParseEnvironment(environment)
			if err != nil {
				return fmt.Errorf("%w: --%s: %w", errInvalidInput, environmentFlag, err)
			}
			return runAdmin(command, func(ctx context.Context, application *app.App) error {
				_, secret, err := application.APIKeys.Create(ctx, keyEnvironment, name, apikeys.Scope(scope), nil)
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(command.OutOrStdout(), secret)
				return err
			})
		},
	}
	create.Flags().StringVar(&environment, environmentFlag, "", "environment the key acts in, test or live (required)")
	create.Flags().StringVar(&scope, scopeFlag, "", "runtime for the runtime routes, admin for the runtime and admin routes (required)")
	create.Flags().StringVar(&name, nameFlag, "", "name that tells keys apart, 1 to 80 characters (required)")
	return create
}
