// Package prompt asks the user questions on the terminal (spec 0.3, F11.4, F15.2, F19).
//
// Steps depend on the Prompter interface, never on the terminal library, so tests script the answers with
// Scripted. NewTerminal is the real implementation, built on charm.land/huh/v2.
package prompt

import (
	"context"

	"github.com/celest-io/remeris/internal/diag"
)

const (
	codePrompt = "remeris::prompt"

	// errorMessage and errorHelp are Knope's wording (crates/knope/src/prompt.rs).
	errorMessage = "Failed to get user input"
	errorHelp    = "This step requires user input, but no user input was provided. Try running the step again."
)

// Prompter asks questions and returns the answers. Every method returns an error built by Error when the
// answer cannot be obtained (no terminal, interrupt, cancelled context).
type Prompter interface {
	// Select shows options and returns the index of the chosen one. options must not be empty.
	Select(ctx context.Context, message string, options []string) (int, error)
	// MultiSelect shows options and returns the indexes of the chosen ones, in option order. When none is
	// chosen, the prompt shows atLeastOne as the validation message and asks again.
	MultiSelect(ctx context.Context, message string, options []string, atLeastOne string) ([]int, error)
	// Input asks for a line of text; help is shown under the question and may be empty. The answer may be empty.
	Input(ctx context.Context, message, help string) (string, error)
	// Password asks for a secret line of text. Nothing is echoed and there is no confirmation.
	Password(ctx context.Context, message string) (string, error)
}

// Error returns the diagnostic every prompt failure uses: Knope's "Failed to get user input" with its help
// text. cause says why (for example that the input is not a terminal) and stays reachable with errors.Is/As.
func Error(cause error) *diag.Error {
	return diag.New(errorMessage).WithCode(codePrompt).WithHelp(errorHelp).WithCause(cause)
}
