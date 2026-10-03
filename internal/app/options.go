// Package app holds remeris' runtime options and the loader that fills them from the command
// line and the environment (spec F3.1, the readykit pattern).
package app

// GlobalOptions are the root command's flags.
type GlobalOptions struct {
	DryRun   bool `mapstructure:"dry-run"`
	Verbose  bool `mapstructure:"verbose"`
	Generate bool `mapstructure:"generate"`
	Upgrade  bool `mapstructure:"upgrade"`
	Validate bool `mapstructure:"validate"`
}

// DefaultGlobalOptions returns the options in effect when no flag is set.
func DefaultGlobalOptions() *GlobalOptions { return &GlobalOptions{} }

// WorkflowOptions are the flags of a workflow subcommand.
type WorkflowOptions struct {
	// OverrideVersion holds `<version>` (one package) or `name=<version>` values (several).
	OverrideVersion []string `mapstructure:"override-version"`
	// PrereleaseLabel overrides `prerelease_label` of every PrepareRelease step.
	PrereleaseLabel string `mapstructure:"prerelease-label"`
}

// DefaultWorkflowOptions returns the options in effect when no flag is set.
func DefaultWorkflowOptions() *WorkflowOptions { return &WorkflowOptions{} }
