package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const (
	exitSuccess      = 0
	exitFailure      = 1
	exitInvalidInput = 2
)

var errInvalidInput = errors.New("invalid input")

func main() {
	os.Exit(exitCode(newRootCommand().Execute()))
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "preburn",
		Short: "Preburn server, worker and administration commands",
		Args:  noArguments,
		RunE: func(command *cobra.Command, _ []string) error {
			return command.Help()
		},
		SilenceUsage:      true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
	}
	root.SetFlagErrorFunc(invalidFlag)
	root.AddCommand(
		newServeCommand(),
		newWorkerCommand(),
		newMigrateCommand(),
		newOpenAPICommand(),
		newSecretKeyCommand(),
		newVersionCommand(),
		newAdminCommand(),
		newBenchCommand(),
		newHealthcheckCommand(),
	)
	return root
}

func noArguments(command *cobra.Command, arguments []string) error {
	if err := cobra.NoArgs(command, arguments); err != nil {
		return fmt.Errorf("%w: %w", errInvalidInput, err)
	}
	return nil
}

func invalidFlag(_ *cobra.Command, err error) error {
	return fmt.Errorf("%w: %w", errInvalidInput, err)
}

func exitCode(err error) int {
	if err == nil {
		return exitSuccess
	}
	if errors.Is(err, errInvalidInput) {
		return exitInvalidInput
	}
	return exitFailure
}
