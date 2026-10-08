// Package steps implements the workflow steps (spec F5 onwards), one file per step.
package steps

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/steps/command"
	"github.com/celest-io/remeris/internal/variables"
	"github.com/celest-io/remeris/internal/workflow"
)

// Command runs a command, after substituting variables into it (spec F5).
type Command struct {
	Command string `toml:"command"`
	// Variables is nil when the key is absent, which selects the default variables. See
	// variables.Template.
	Variables []variables.Entry `toml:"variables"`
	// Shell runs the command through the platform shell instead of splitting it into arguments.
	Shell bool `toml:"shell"`
}

func (*Command) Type() string { return "Command" }

func (c *Command) Run(ctx context.Context, rt workflow.RunType[workflow.State]) (workflow.RunType[workflow.State], error) {
	line, err := variables.Template{Text: c.Command, Variables: c.Variables}.Replace(rt.State)
	if err != nil {
		return rt, err
	}
	if rt.DryRun {
		slog.InfoContext(ctx, "Would run "+line)
		return rt, nil
	}

	var cmd *exec.Cmd
	if c.Shell {
		cmd = shellCommand(line)
	} else {
		args := command.Split(line)
		if len(args) == 0 {
			return rt, ioError(errors.New("the command is empty"))
		}
		cmd = exec.Command(args[0], args[1:]...) //nolint:gosec // running the configured command is the purpose of this step
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return rt, diag.New("Command returned non-zero exit code").
				WithCode("command::failed").
				WithHelp("The command failed to execute. Try running it manually to get more information.").
				WithCause(err)
		}
		return rt, ioError(err)
	}
	return rt, nil
}

func ioError(err error) error {
	return diag.Newf("I/O error: %w", err).WithCode("command::io")
}
