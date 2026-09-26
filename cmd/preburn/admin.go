package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"

	"github.com/preburn/preburn/internal/app"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/logging"
)

const bodyLocationPrefix = "body."

func newAdminCommand() *cobra.Command {
	return newGroupCommand("admin", "Create members, setup links, reset links and API keys",
		newAdminCreateCommand(),
		newAdminSetupLinkCommand(),
		newAdminResetPasswordCommand(),
		newAdminAPIKeyCommand(),
	)
}

func newGroupCommand(use string, short string, subcommands ...*cobra.Command) *cobra.Command {
	group := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  noArguments,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
	}
	group.AddCommand(subcommands...)
	return group
}

//nolint:contextcheck // cobra hands commands their context through command.Context(), which the flag closures of RunE cannot take as a parameter.
func runAdmin(command *cobra.Command, run func(ctx context.Context, application *app.App) error) (err error) {
	ctx := command.Context()
	configuration, err := loadConfiguration()
	if err != nil {
		return err
	}
	logger := logging.New(command.ErrOrStderr(), configuration.LogLevel)
	redis.SetLogger(redisLogger{library: logger.Library(redisLibraryName)})
	application, err := app.New(ctx, configuration, app.RoleAdmin, logger)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, application.Close())
	}()
	return invalidInputFromRejection(run(ctx, application))
}

func requiredFlag(name string, value string) error {
	if value == "" {
		return fmt.Errorf("%w: --%s is required", errInvalidInput, name)
	}
	return nil
}

func invalidInputFromRejection(err error) error {
	if problem, isProblem := errors.AsType[*httpapi.Problem](err); isProblem {
		fieldProblems := make([]string, 0, len(problem.Errors))
		for _, fieldError := range problem.Errors {
			fieldProblems = append(fieldProblems, strings.TrimPrefix(fieldError.Location, bodyLocationPrefix)+": "+fieldError.Message)
		}
		return fmt.Errorf("%w: %s", errInvalidInput, strings.Join(fieldProblems, ", "))
	}
	if coded, isCoded := errors.AsType[httpapi.CodedError](err); isCoded && coded.ProblemStatus() < http.StatusInternalServerError {
		return fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	return err
}
