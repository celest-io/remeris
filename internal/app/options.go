// Package app holds remeris' runtime options and the loader that fills them from the command
// line and the environment (spec F3.1, the readykit pattern).
package app

import (
	"fmt"

	"github.com/celest-io/remeris/internal/diag"
)

// Names of the GitHub App flags and of the environment variables that set them (decision D11). The credentials
// come only from these flags and variables, never from knope.toml.
const (
	FlagGitHubAppID          = "github-app-id"
	FlagGitHubInstallationID = "github-installation-id"
	FlagGitHubAppKeyPath     = "github-app-private-key-path"

	EnvGitHubAppID          = "REMERIS_GITHUB_APP_ID"
	EnvGitHubInstallationID = "REMERIS_GITHUB_INSTALLATION_ID"
	EnvGitHubAppKeyPath     = "REMERIS_GITHUB_APP_PRIVATE_KEY_PATH"
	// EnvGitHubAppKey holds the PEM content itself, for CI secrets. It is read directly and has no flag.
	EnvGitHubAppKey = "REMERIS_GITHUB_APP_PRIVATE_KEY"

	codeGitHubApp = "remeris::github::app_options"
)

// GlobalOptions are the root command's flags.
type GlobalOptions struct {
	DryRun   bool `mapstructure:"dry-run"`
	Verbose  bool `mapstructure:"verbose"`
	Generate bool `mapstructure:"generate"`
	Upgrade  bool `mapstructure:"upgrade"`
	Validate bool `mapstructure:"validate"`

	// GitHub App authentication (F16.1). Never read from knope.toml.
	GitHubAppID          int64  `mapstructure:"github-app-id"`
	GitHubInstallationID int64  `mapstructure:"github-installation-id"`
	GitHubAppKeyPath     string `mapstructure:"github-app-private-key-path"`
}

// GitHubAppPrivateKey returns the PEM bytes of the GitHub App private key and whether one is configured.
// The file at GitHubAppKeyPath wins over REMERIS_GITHUB_APP_PRIVATE_KEY: a path is an explicit choice made on the
// command line (or in a variable meant for it), while the key variable is usually a leftover CI secret. The
// content is not parsed here. A read error names the path and never the content.
func (o *GlobalOptions) GitHubAppPrivateKey(
	getenv func(string) string, readFile func(string) ([]byte, error),
) (key []byte, found bool, err error) {
	if o.GitHubAppKeyPath != "" {
		key, err = readFile(o.GitHubAppKeyPath)
		if err != nil {
			return nil, false, diag.Newf("Could not read the GitHub App private key at %s", o.GitHubAppKeyPath).
				WithCode(codeGitHubApp).
				WithHelp("Check the path given to --" + FlagGitHubAppKeyPath + " or " + EnvGitHubAppKeyPath + ".").
				WithCause(err)
		}
		return key, true, nil
	}
	if v := getenv(EnvGitHubAppKey); v != "" {
		return []byte(v), true, nil
	}
	return nil, false, nil
}

// ValidateGitHubApp checks that the GitHub App options go together: an App ID needs a private key (a path or
// REMERIS_GITHUB_APP_PRIVATE_KEY), and a key or an installation ID needs an App ID. All unset is fine.
func (o *GlobalOptions) ValidateGitHubApp(getenv func(string) string) error {
	hasKey := o.GitHubAppKeyPath != "" || getenv(EnvGitHubAppKey) != ""
	flags := fmt.Sprintf("--%s/%s, --%s/%s and %s", FlagGitHubAppID, EnvGitHubAppID,
		FlagGitHubAppKeyPath, EnvGitHubAppKeyPath, EnvGitHubAppKey)
	switch {
	case o.GitHubAppID != 0 && !hasKey:
		return diag.New("A GitHub App ID is set without a private key").
			WithCode(codeGitHubApp).
			WithHelp("Also set --" + FlagGitHubAppKeyPath + " (" + EnvGitHubAppKeyPath + ") or " + EnvGitHubAppKey + ".")
	case o.GitHubAppID == 0 && hasKey:
		return diag.New("A GitHub App private key is set without an App ID").
			WithCode(codeGitHubApp).
			WithHelp("Also set --" + FlagGitHubAppID + " (" + EnvGitHubAppID + "), or remove the key options: " + flags + ".")
	case o.GitHubAppID == 0 && o.GitHubInstallationID != 0:
		return diag.New("A GitHub App installation ID is set without an App ID").
			WithCode(codeGitHubApp).
			WithHelp("Also set --" + FlagGitHubAppID + " (" + EnvGitHubAppID + "), or remove --" +
				FlagGitHubInstallationID + " (" + EnvGitHubInstallationID + ").")
	}
	return nil
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
