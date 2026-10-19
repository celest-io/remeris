// Package cli builds the command line: a root command with one subcommand per workflow (spec F3.1).
package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/celest-io/remeris/internal/app"
	"github.com/celest-io/remeris/internal/config"
	"github.com/celest-io/remeris/internal/credentials"
	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/forge/github"
	"github.com/celest-io/remeris/internal/project"
	"github.com/celest-io/remeris/internal/prompt"
	"github.com/celest-io/remeris/internal/steps"
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
			if cfg.GitHub == nil {
				return ctx, nil // the App options are only used with [github]
			}
			return ctx, global.ValidateGitHubApp(os.Getenv)
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if handled, err := runSpecialMode(ctx, global, cfg, nil); handled {
				return err
			}
			// Knope builds the state, and so reports package errors, before it looks for the workflow.
			if _, err := project.Load(ctx, cfg, projectOptions(global, nil)); err != nil {
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
		&cli.Int64Flag{
			Name:    app.FlagGitHubAppID,
			Usage:   "ID of the GitHub App to authenticate as",
			Sources: cli.EnvVars(app.EnvGitHubAppID),
		},
		&cli.Int64Flag{
			Name:    app.FlagGitHubInstallationID,
			Usage:   "ID of the GitHub App installation",
			Sources: cli.EnvVars(app.EnvGitHubInstallationID),
		},
		&cli.StringFlag{
			Name: app.FlagGitHubAppKeyPath,
			Usage: "Path to the GitHub App private key (PEM); " + app.EnvGitHubAppKey +
				" may hold the key itself",
			Sources: cli.EnvVars(app.EnvGitHubAppKeyPath),
		},
		&cli.StringFlag{
			Name: app.FlagGitHubAPIURL,
			Usage: "API base URL of the GitHub instance to send credentials to, for GitHub Enterprise. " +
				"The api_url of [github] must be the same URL to be used",
			Sources: cli.EnvVars(app.EnvGitHubAPIURL),
		},
		&cli.StringFlag{
			Name: app.FlagGitHubUploadURL,
			Usage: "Upload base URL of the GitHub instance, when it is not the default for the API URL. " +
				"The upload_url of [github] must be the same URL to be used",
			Sources: cli.EnvVars(app.EnvGitHubUploadURL),
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
		// A comma is not a separator: "a=2.0.0,b=3.0.0" is one (invalid) value, as in Knope.
		DisableSliceFlagSeparator: true,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			// The override versions are stored on the packages and used by BumpVersion and PrepareRelease.
			opts := app.DefaultWorkflowOptions()
			if err := app.LoadConfig(cmd, opts); err != nil {
				return err
			}
			setPrereleaseLabel(cfg, opts.PrereleaseLabel)
			if handled, err := runSpecialMode(ctx, global, cfg, opts.OverrideVersion); handled {
				return err
			}
			prompter := prompt.NewTerminal(os.Stdin, os.Stdout)
			// A failure to discover the packages matters only to a workflow that reads them (spec F20.1).
			if w.NeedsPackages() {
				if err := cfg.DiscoveryError(); err != nil {
					return err
				}
			}
			state, err := project.Load(ctx, cfg, projectOptions(global, prompter))
			if err != nil {
				return err
			}
			if err := applyOverrides(state.Packages, opts.OverrideVersion); err != nil {
				return err
			}
			state.Prompter = prompter
			rt := workflow.Real(state)
			if global.DryRun {
				rt = workflow.DryRun(state)
			}
			return workflow.Run(ctx, w, rt)
		},
	}
}

// setPrereleaseLabel sets prerelease_label on every PrepareRelease step of every workflow (spec F3.1). The flag
// beats the environment variables, which urfave/cli reads as the flag's sources. An empty label changes nothing.
func setPrereleaseLabel(cfg *config.Config, label string) {
	if label == "" {
		return
	}
	for _, w := range cfg.Workflows {
		for _, step := range w.Steps {
			if p, ok := step.(*steps.PrepareRelease); ok {
				p.PrereleaseLabel = label
			}
		}
	}
}

// runSpecialMode handles --generate, --upgrade and --validate, which take precedence over a
// workflow. handled is false when none of them was given. --validate loads the packages and applies the
// --override-version values (those of the workflow command, if one was given) first, as Knope does.
func runSpecialMode(
	ctx context.Context, global *app.GlobalOptions, cfg *config.Config, overrides []string,
) (handled bool, err error) {
	switch {
	case global.Validate:
		if err := cfg.DiscoveryError(); err != nil {
			return true, err
		}
		state, err := project.Load(ctx, cfg, projectOptions(global, nil))
		if err != nil {
			return true, err
		}
		if err := applyOverrides(state.Packages, overrides); err != nil {
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

// projectOptions says how the GitHub forge authenticates: with the GitHub App of the global options, if any,
// else with GITHUB_TOKEN or the stored token, else (when p can ask) with a token typed by the user. The private
// key is read only when a client is built, never in a dry run.
func projectOptions(global *app.GlobalOptions, p prompt.Prompter) project.Options {
	opts := project.Options{
		Credentials:            &credentials.Store{Prompter: p},
		TrustedGitHubAPIURL:    global.GitHubAPIURL,
		TrustedGitHubUploadURL: global.GitHubUploadURL,
	}
	if global.GitHubAppID != 0 {
		opts.GitHubApp = &github.App{
			ID:             global.GitHubAppID,
			InstallationID: global.GitHubInstallationID,
			PrivateKey: func() ([]byte, error) {
				key, found, err := global.GitHubAppPrivateKey(os.Getenv, os.ReadFile)
				if err == nil && !found {
					err = diag.New("A GitHub App ID is set without a private key").WithCode("remeris::github::app_options")
				}
				return key, err
			},
		}
	}
	return opts
}

func notImplemented(what, milestone string) error {
	return diag.Newf("%s is not implemented yet (milestone %s)", what, milestone).
		WithCode(name + "::not_implemented")
}

// validate runs every workflow as a dry run, each on its own copy of the state, and reports all the problems at
// once (spec F21).
func validate(ctx context.Context, cfg *config.Config, state workflow.State) error {
	var problems []error
	for _, w := range cfg.Workflows {
		if err := workflow.Run(ctx, w, workflow.DryRun(state.Clone())); err != nil {
			problems = append(problems, err)
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return diag.New("There are problems with the defined workflows").WithRelated(problems...)
}
