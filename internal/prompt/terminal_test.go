package prompt_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/prompt"
)

func TestTerminalNotATTY(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tests := map[string]func(prompt.Prompter) error{
		"select": func(p prompt.Prompter) error { _, err := p.Select(ctx, "q", abc); return err },
		"multiselect": func(p prompt.Prompter) error {
			_, err := p.MultiSelect(ctx, "q", abc, msgAtLeast)
			return err
		},
		"input":    func(p prompt.Prompter) error { _, err := p.Input(ctx, "q", ""); return err },
		"password": func(p prompt.Prompter) error { _, err := p.Password(ctx, "q"); return err },
	}
	for name, ask := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			err := ask(prompt.NewTerminal(strings.NewReader("1\n"), &out))
			if !errors.Is(err, prompt.ErrNotTTY) {
				t.Fatalf("err = %v, want ErrNotTTY", err)
			}
			var d diag.Diagnostic
			if !errors.As(err, &d) {
				t.Fatalf("%T is not a diag.Diagnostic", err)
			}
			if d.Error() != "Failed to get user input" || d.Code() != "remeris::prompt" ||
				d.Help() != "This step requires user input, but no user input was provided. Try running the step again." {
				t.Errorf("diagnostic = %q %q %q", d.Error(), d.Code(), d.Help())
			}
			if out.Len() != 0 {
				t.Errorf("wrote %q before failing", out.String())
			}
		})
	}
}

func TestTerminalEmptyOptions(t *testing.T) {
	t.Parallel()
	p := prompt.NewTerminal(strings.NewReader(""), &bytes.Buffer{})
	if _, err := p.Select(context.Background(), "q", nil); err == nil {
		t.Error("Select with no options succeeded")
	}
	if _, err := p.MultiSelect(context.Background(), "q", nil, msgAtLeast); err == nil {
		t.Error("MultiSelect with no options succeeded")
	}
}
