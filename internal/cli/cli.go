// Package cli builds the command line: a root command with one subcommand per workflow (spec F3.1).
package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/urfave/cli/v3"

	"github.com/celest-io/remeris/internal/app"
	"github.com/celest-io/remeris/internal/config"
	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/project"
	"github.com/celest-io/remeris/internal/version"
	"github.com/celest-io/remeris/internal/workflow"
)

// name is the program name, used in help, errors and diagnostic codes.
const name = "remeris"

// Environment variables that set --prerelease-label. Remeris' own name wins; KNOPE_PRERELEASE_LABEL is the
// name Knope's configuration documents for the same setting.
const (
	prereleaseLabelEnv      = "REMERIS_PRERELEASE_LABEL"
	knopePrereleaseLabelEnv = "KNOPE_PRERELEASE_LABEL"
)

// Main loads the configuration, runs the command line in args and returns the process exit code.
// Logs go to stdout, rendered errors to stderr.
func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	cfg, err := config.Load(config.KnopeFile)
	if err == nil {
		err = New(cfg, stdout, stderr).Run(ctx, args)
	}
	if err != nil {
		diag.Render(stderr, err)
		return 1
	}
	return 0
}

// New builds the root command for cfg.
func New(cfg *config.Config, stdout, stderr io.Writer) *cli.Command {
	global := app.DefaultGlobalOptions()

	// urfave's default version flag claims -v, which is --verbose here.
	cli.VersionFlag = &cli.BoolFlag{
		Name:        "version",
		Aliases:     []string{"V"},
		Usage:       "Print version",
		HideDefault: true,
		Local:       true,
	}
	cli.VersionPrinter = func(cmd *cli.Command) {
		_, _ = fmt.Fprintf(cmd.Root().Writer, "%s %s\n", cmd.Root().Name, cmd.Root().Version)
	}

	root := &cli.Command{
		Name:                  name,
		Usage:                 "Automate releases and change management",
		Version:               version.Full(),
		Writer:                stdout,
		ErrWriter:             stderr,
		Suggest:               true,
		EnableShellCompletion: true,
		Flags:                 rootFlags(cfg),
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			if err := app.LoadConfig(cmd, global); err != nil {
				return ctx, err
			}
			setupLogging(stdout, global.Verbose, global.Validate)
			return ctx, nil
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if handled, err := runSpecialMode(ctx, global, cfg); handled {
				return err
			}
			// Knope builds the state, and so reports package errors, before it looks for the workflow.
			if _, err := project.Load(ctx, cfg); err != nil {
				return err
			}
			if cmd.Args().Present() {
				return diag.Newf("No workflow named %s", cmd.Args().First())
			}
			return diag.Newf("No workflow selected. Run '%s --help' for a list of options.", name)
		},
	}
	for _, w := range cfg.Workflows {
		root.Commands = append(root.Commands, workflowCommand(cfg, global, w))
	}
	return root
}

func rootFlags(cfg *config.Config) []cli.Flag {
	flags := []cli.Flag{
		&cli.BoolFlag{
			Name:  "dry-run",
			Usage: "Pretend to run a workflow, outputting what _would_ happen without actually doing it.",
		},
		&cli.BoolFlag{
			Name:    "verbose",
			Aliases: []string{"v"},
			Usage:   "Print extra information (for debugging)",
		},
	}
	if cfg.Source == config.SourceDefault {
		return append(flags, &cli.BoolFlag{Name: "generate", Local: true, Usage: "Generate a " + config.KnopeFile + " file"})
	}
	return append(flags,
		&cli.BoolFlag{
			Name: "upgrade", Local: true,
			Usage: "Upgrade to the latest " + config.KnopeFile + " syntax from any deprecated (but still supported) syntax.",
		},
		&cli.BoolFlag{Name: "validate", Local: true, Usage: "Check that the " + config.KnopeFile + " file is valid."},
	)
}

// workflowCommand turns a workflow into a subcommand. Its flags depend on the steps and on how many
// packages the configuration declares.
func workflowCommand(cfg *config.Config, global *app.GlobalOptions, w workflow.Workflow) *cli.Command {
	var flags []cli.Flag
	hasBump, hasPrepare := w.HasStep("BumpVersion"), w.HasStep("PrepareRelease")
	if (hasBump || hasPrepare) && cfg.PackageCount() > 0 {
		usage := "Override the version set by BumpVersion or PrepareRelease for the package."
		if cfg.PackageCount() > 1 {
			usage = "Override the version set by BumpVersion or PrepareRelease for multiple packages. " +
				"Format is like package_name=version, can be set multiple times."
		}
		flags = append(flags, &cli.StringSliceFlag{Name: "override-version", Usage: usage})
	}
	if hasPrepare {
		flags = append(flags, &cli.StringFlag{
			Name:    "prerelease-label",
			Usage:   "Set the prerelease_label attribute of any PrepareRelease steps at runtime.",
			Sources: cli.EnvVars(prereleaseLabelEnv, knopePrereleaseLabelEnv),
		})
	}

	return &cli.Command{
		Name:  w.Name,
		Usage: w.HelpText,
		Flags: flags,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			// Parsed now so the values are validated and ready; they take effect with
			// BumpVersion (M3) and PrepareRelease (M5).
			opts := app.DefaultWorkflowOptions()
			if err := app.LoadConfig(cmd, opts); err != nil {
				return err
			}
			if handled, err := runSpecialMode(ctx, global, cfg); handled {
				return err
			}
			state, err := project.Load(ctx, cfg)
			if err != nil {
				return err
			}
			rt := workflow.Real(state)
			if global.DryRun {
				rt = workflow.DryRun(state)
			}
			return workflow.Run(ctx, w, rt)
		},
	}
}

// runSpecialMode handles --generate, --upgrade and --validate, which take precedence over a
// workflow. handled is false when none of them was given. --validate loads the packages first, as Knope does.
func runSpecialMode(ctx context.Context, global *app.GlobalOptions, cfg *config.Config) (handled bool, err error) {
	switch {
	case global.Validate:
		state, err := project.Load(ctx, cfg)
		if err != nil {
			return true, err
		}
		return true, validate(ctx, cfg, state)
	case global.Generate:
		return true, notImplemented("--generate", "M10")
	case global.Upgrade:
		return true, notImplemented("--upgrade", "M10")
	}
	return false, nil
}

func notImplemented(what, milestone string) error {
	return diag.Newf("%s is not implemented yet (milestone %s)", what, milestone).
		WithCode(name + "::not_implemented")
}

// validate runs every workflow as a dry run and reports all the problems at once (spec F21).
func validate(ctx context.Context, cfg *config.Config, state workflow.State) error {
	var problems []error
	for _, w := range cfg.Workflows {
		if err := workflow.Run(ctx, w, workflow.DryRun(state)); err != nil {
			problems = append(problems, err)
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return diag.New("There are problems with the defined workflows").WithRelated(problems...)
}
