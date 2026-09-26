package main

import (
	"errors"
	"io"
	"testing"
)

func TestUnknownSubcommandIsInvalidInput(t *testing.T) {
	root := newRootCommand()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"unknown"})

	err := root.Execute()
	if err == nil {
		t.Fatal("execute unknown: got no error")
	}
	if code := exitCode(err); code != exitInvalidInput {
		t.Errorf("exit code = %d, want %d", code, exitInvalidInput)
	}
}

func TestExitCodeForArguments(t *testing.T) {
	tests := []struct {
		name      string
		arguments []string
		want      int
	}{
		{name: "no arguments", arguments: []string{}, want: exitSuccess},
		{name: "unknown flag", arguments: []string{"--unknown"}, want: exitInvalidInput},
		{name: "unknown subcommand flag", arguments: []string{"version", "--unknown"}, want: exitInvalidInput},
		{name: "extra argument", arguments: []string{"version", "extra"}, want: exitInvalidInput},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := newRootCommand()
			root.SetOut(io.Discard)
			root.SetErr(io.Discard)
			root.SetArgs(test.arguments)

			if code := exitCode(root.Execute()); code != test.want {
				t.Errorf("exit code = %d, want %d", code, test.want)
			}
		})
	}
}

func TestExitCodeForFailure(t *testing.T) {
	if code := exitCode(errors.New("database unreachable")); code != exitFailure {
		t.Errorf("exit code = %d, want %d", code, exitFailure)
	}
}
