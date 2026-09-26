package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/preburn/preburn/internal/app"
	"github.com/preburn/preburn/internal/httpapi"
	"github.com/preburn/preburn/internal/identifiers"
	"github.com/preburn/preburn/internal/installation"
)

const (
	emailFlag         = "email"
	nameFlag          = "name"
	passwordStdinFlag = "password-stdin"
	passwordPrompt    = "Password: "
)

func newAdminCreateCommand() *cobra.Command {
	var email, name string
	var passwordFromStandardInput bool
	create := &cobra.Command{
		Use:   "create",
		Short: "Create a member with a password, completing setup when it is pending, and print the member id",
		Args:  noArguments,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := errors.Join(requiredFlag(emailFlag, email), requiredFlag(nameFlag, name)); err != nil {
				return err
			}
			password, err := readPassword(command, passwordFromStandardInput)
			if err != nil {
				return err
			}
			return runAdmin(command, func(ctx context.Context, application *app.App) error {
				member, err := application.Installation.CreateAdmin(ctx, email, name, password)
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(command.OutOrStdout(), identifiers.Encode(identifiers.PrefixMember, member.ID))
				return err
			})
		},
	}
	create.Flags().StringVar(&email, emailFlag, "", "email address the member signs in with (required)")
	create.Flags().StringVar(&name, nameFlag, "", "display name of the member, 1 to 80 characters (required)")
	create.Flags().BoolVar(&passwordFromStandardInput, passwordStdinFlag, false, "read the password from standard input instead of prompting on the terminal")
	return create
}

func newAdminSetupLinkCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "setup-link",
		Short: "Replace the setup link and print it while setup is pending",
		Args:  noArguments,
		RunE: func(command *cobra.Command, _ []string) error {
			return runAdmin(command, func(ctx context.Context, application *app.App) error {
				link, pending, err := application.Installation.PrepareSetupLink(ctx)
				if err != nil {
					return err
				}
				if !pending {
					return installation.ErrSetupNotAvailable
				}
				_, err = fmt.Fprintln(command.OutOrStdout(), link)
				return err
			})
		},
	}
}

func newAdminResetPasswordCommand() *cobra.Command {
	var email string
	resetPassword := &cobra.Command{
		Use:   "reset-password",
		Short: "Print a password reset link for a member, valid for 24 hours",
		Args:  noArguments,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := requiredFlag(emailFlag, email); err != nil {
				return err
			}
			return runAdmin(command, func(ctx context.Context, application *app.App) error {
				member, err := application.Members.MemberByEmail(ctx, email)
				if errors.Is(err, httpapi.ErrNotFound) {
					return fmt.Errorf("%w: no member has this email", errInvalidInput)
				}
				if err != nil {
					return err
				}
				link, err := application.Members.CreateResetLink(ctx, member.ID, nil)
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(command.OutOrStdout(), link)
				return err
			})
		},
	}
	resetPassword.Flags().StringVar(&email, emailFlag, "", "email address of the member (required)")
	return resetPassword
}

func readPassword(command *cobra.Command, fromStandardInput bool) (string, error) {
	input := command.InOrStdin()
	if fromStandardInput {
		contents, err := io.ReadAll(input)
		if err != nil {
			return "", fmt.Errorf("read password from standard input: %w", err)
		}
		return strings.TrimRight(string(contents), "\r\n"), nil
	}
	terminal, isFile := input.(*os.File)
	if !isFile || !term.IsTerminal(int(terminal.Fd())) {
		return "", fmt.Errorf("%w: standard input is not a terminal, pass --%s", errInvalidInput, passwordStdinFlag)
	}
	if _, err := fmt.Fprint(command.ErrOrStderr(), passwordPrompt); err != nil {
		return "", err
	}
	password, err := term.ReadPassword(int(terminal.Fd()))
	if err != nil {
		return "", fmt.Errorf("read password from terminal: %w", err)
	}
	if _, err := fmt.Fprintln(command.ErrOrStderr()); err != nil {
		return "", err
	}
	return string(password), nil
}
