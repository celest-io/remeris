// Package credentials finds the secrets remeris needs from outside a release configuration: forge tokens and
// the user's email address (spec F15.2).
//
// A value comes from, in order: an environment variable (tokens only), the file <config>/remeris/<key>, the file
// <config>/knope/<key> (copied into the remeris directory, so existing Knope users are not asked again), and
// finally the user, whose answer is saved to <config>/remeris/<key>. Files hold the raw value, with no trimming,
// as Knope's do. Values are never logged.
//
// Reading can write: finding a value in the Knope directory copies it into the remeris one. A dry run must
// therefore not look credentials up; the GitHub client is never built in a dry run.
//
// An empty environment variable counts as unset, where Knope would use the empty string as the token. So does a
// stored file that holds only white space, and a blank answer to the prompt is refused and never saved: it would
// otherwise be found at every later run, which would then never ask again.
package credentials

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/prompt"
)

const (
	// Directory names under the user config directory.
	dirName       = "remeris"
	legacyDirName = "knope"

	dirMode  fs.FileMode = 0o700
	fileMode fs.FileMode = 0o600

	// storeHelp is Knope's help text for configuration problems, with the program name rewritten.
	storeHelp = "Remeris attempts to store config in a local config directory, this error may be a permissions issue " +
		"or may mean you're using an obscure operating system"
)

// Key names one credential: its file name, the environment variable that overrides it (empty for none) and the
// question asked when it is missing. The prompt texts are Knope's (crates/knope/src/app_config.rs).
type Key struct {
	name   string
	env    string
	prompt string
	// label names the value in the error for a blank answer: "No {label} was given".
	label string
}

// Name returns the file name of the key.
func (k Key) Name() string { return k.name }

// Env returns the environment variable that overrides the key, or "".
func (k Key) Env() string { return k.env }

// Prompt returns the question asked when the key is missing.
func (k Key) Prompt() string { return k.prompt }

// Keys that do not depend on a host.
var (
	// GitHubToken is the GitHub personal access token, overridden by GITHUB_TOKEN.
	GitHubToken = Key{
		name:  "github_token",
		env:   "GITHUB_TOKEN",
		label: "GitHub token",
		prompt: "No GitHub token found, generate one from https://github.com/settings/tokens " +
			"with `repo` permissions and input here",
	}
	// JiraToken is the Jira API token.
	JiraToken = Key{
		name:  "jira_token",
		label: "Jira token",
		prompt: "No Jira token found, generate one from https://id.atlassian.com/manage-profile/security/api-tokens " +
			"and input here",
	}
	// Email is the user's email address (Jira basic auth). Knope asks for it with the same masked prompt as tokens.
	Email = Key{name: "email", prompt: "Input your email address", label: "email address"}
)

// GiteaToken returns the Gitea token key, overridden by GITEA_TOKEN. The stored value is shared by every host:
// host only appears in the prompt text.
func GiteaToken(host string) Key {
	return Key{
		name:  "gitea_token",
		env:   "GITEA_TOKEN",
		label: "Gitea token",
		prompt: "No Gitea token found, generate one from " + host + "/user/settings/applications with\n" +
			"`repository` permissions set to `Read and Write` and `issue` permissions set to `Read` and input here",
	}
}

// Store reads and writes credentials. The zero value works: it uses the real environment and the user config
// directory, and cannot prompt (Get then fails like a non-interactive session).
type Store struct {
	// Dir is the base config directory holding the remeris and knope directories. Empty means os.UserConfigDir.
	Dir string
	// Getenv reads environment variables. Nil means os.Getenv.
	Getenv func(string) string
	// Prompter asks the user. Nil means no user input is available.
	Prompter prompt.Prompter
}

// Lookup finds the value of key without ever asking the user: environment, then the remeris file, then the
// Knope file (copied into the remeris directory). found is false when none has it. An empty environment
// variable counts as unset, where Knope would use the empty string.
//
// Lookup may write: the migration copy creates the remeris directory and file. A dry run must not call it.
// When the copy fails, the Knope value is still returned and the failure is logged at debug level, without
// the value.
func (s *Store) Lookup(ctx context.Context, key Key) (value string, found bool, err error) {
	if key.env != "" {
		if v := s.getenv(key.env); !blank(v) {
			return v, true, nil
		}
	}
	base, err := s.baseDir()
	if err != nil {
		return "", false, err
	}
	v, ok, err := readValue(filepath.Join(base, dirName, key.name))
	if err != nil || (ok && !blank(v)) {
		return v, ok, err
	}
	v, ok, err = readValue(filepath.Join(base, legacyDirName, key.name))
	if err != nil || !ok || blank(v) {
		return "", false, err
	}
	dir := filepath.Join(base, dirName)
	if err := writeValue(dir, key.name, v); err != nil {
		slog.DebugContext(ctx, "Could not copy the Knope credential "+key.name+" to "+dir+": "+err.Error())
	}
	return v, true, nil
}

// Get returns the value of key: what Lookup finds, or else the user's answer, which is saved for next time.
func (s *Store) Get(ctx context.Context, key Key) (string, error) {
	v, found, err := s.Lookup(ctx, key)
	if err != nil || found {
		return v, err
	}
	if s.Prompter == nil {
		return "", prompt.Error(errors.New("no terminal is available"))
	}
	answer, err := s.Prompter.Password(ctx, key.prompt)
	if err != nil {
		var d diag.Diagnostic
		if errors.As(err, &d) {
			return "", err
		}
		return "", prompt.Error(err)
	}
	if blank(answer) {
		return "", errBlankAnswer(key)
	}
	base, err := s.baseDir()
	if err != nil {
		return "", err
	}
	if err := writeValue(filepath.Join(base, dirName), key.name, answer); err != nil {
		return "", err
	}
	return answer, nil
}

// blank reports whether v is empty or only white space.
func blank(v string) bool { return strings.TrimSpace(v) == "" }

// errBlankAnswer is the error for a blank answer to the prompt.
func errBlankAnswer(key Key) *diag.Error {
	help := "Enter a value when asked."
	if key.env != "" {
		help = "Set the " + key.env + " environment variable or enter a token when asked."
	}
	return diag.New("No " + key.label + " was given").WithCode("remeris::app_config::blank_answer").WithHelp(help)
}

func (s *Store) getenv(name string) string {
	if s.Getenv != nil {
		return s.Getenv(name)
	}
	return os.Getenv(name)
}

func (s *Store) baseDir() (string, error) {
	if s.Dir != "" {
		return s.Dir, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", errOpenConfigPath(err)
	}
	return dir, nil
}

// readValue returns the raw contents of path; ok is false when the file does not exist.
func readValue(path string) (value string, ok bool, err error) {
	b, err := os.ReadFile(path) //nolint:gosec // G304: the path is built from the config directory and a fixed key name
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, errOpenConfigPath(err)
	}
	return string(b), true, nil
}

func writeValue(dir, name, value string) error {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return diag.New(fmt.Sprintf("Could not create directory %s: %v", dir, err)).
			WithCode("remeris::app_config::could_not_create_directory").
			WithHelp("Failed to create the configuration directory, this is likely a permissions error.").
			WithCause(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(value), fileMode); err != nil {
		return diag.New("Could not write config: " + err.Error()).
			WithCode("remeris::app_config::could_not_write_config").
			WithHelp(storeHelp).
			WithCause(err)
	}
	return nil
}

func errOpenConfigPath(cause error) *diag.Error {
	return diag.New("Could not open configuration path").
		WithCode("remeris::app_config::could_not_open_config_path").
		WithHelp(storeHelp).
		WithCause(cause)
}
