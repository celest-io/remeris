package prompt

import (
	"context"
	"errors"
	"io"
	"os"

	"charm.land/huh/v2"
	"github.com/charmbracelet/x/term"
)

var (
	// ErrNotTTY is the cause when the input is not a terminal. It is inquire's NotTTY message.
	ErrNotTTY = errors.New("The input device is not a TTY") //nolint:revive,staticcheck // Knope's exact wording.
	// ErrInterrupted is the cause when the user interrupts a prompt (Ctrl-C or Esc). It is inquire's
	// Interrupted message; huh does not tell the two keys apart.
	ErrInterrupted = errors.New("Operation was interrupted by the user") //nolint:revive,staticcheck // Knope's wording.
	errNoOptions   = errors.New("there is nothing to choose from")
)

type terminal struct {
	in         io.Reader
	out        io.Writer
	accessible bool
}

// NewTerminal returns the interactive Prompter. Like Knope (inquire), it refuses to prompt when in is not a
// terminal: every method then returns Error(ErrNotTTY). Only an *os.File that is a terminal qualifies.
func NewTerminal(in io.Reader, out io.Writer) Prompter {
	return &terminal{in: in, out: out}
}

// NewAccessible returns a Prompter that uses huh's line-based accessible mode (no cursor movement, answers are
// typed as numbers or text, one per line). It works on any reader, terminal or not, so it suits screen readers
// and scripted input. Knope has no equivalent: NewTerminal never selects it on its own.
func NewAccessible(in io.Reader, out io.Writer) Prompter {
	return &terminal{in: in, out: out, accessible: true}
}

// ready fails with ErrNotTTY when the input is not a terminal. Accessible mode reads plain lines, so it only
// needs a terminal for a secret: huh reads that one with terminal calls and would silently return "" otherwise.
func (t *terminal) ready(secret bool) error {
	if t.accessible && !secret {
		return nil
	}
	if f, ok := t.in.(*os.File); ok && term.IsTerminal(f.Fd()) {
		return nil
	}
	return Error(ErrNotTTY)
}

func (t *terminal) run(ctx context.Context, field huh.Field, secret bool) error {
	if err := t.ready(secret); err != nil {
		return err
	}
	form := huh.NewForm(huh.NewGroup(field)).WithInput(t.in).WithOutput(t.out).WithAccessible(t.accessible)
	if err := form.RunWithContext(ctx); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return Error(ErrInterrupted)
		}
		return Error(err)
	}
	return nil
}

func indexedOptions(options []string) []huh.Option[int] {
	opts := make([]huh.Option[int], len(options))
	for i, o := range options {
		opts[i] = huh.NewOption(o, i)
	}
	return opts
}

func (t *terminal) Select(ctx context.Context, message string, options []string) (int, error) {
	if len(options) == 0 {
		return 0, Error(errNoOptions)
	}
	var idx int
	err := t.run(ctx, huh.NewSelect[int]().Title(message).Options(indexedOptions(options)...).Value(&idx), false)
	return idx, err
}

func (t *terminal) MultiSelect(ctx context.Context, message string, options []string, atLeastOne string) ([]int, error) {
	if len(options) == 0 {
		return nil, Error(errNoOptions)
	}
	var idx []int
	field := huh.NewMultiSelect[int]().Title(message).Options(indexedOptions(options)...).Value(&idx).
		Validate(func(chosen []int) error {
			if len(chosen) == 0 {
				return errors.New(atLeastOne)
			}
			return nil
		})
	if err := t.run(ctx, field, false); err != nil {
		return nil, err
	}
	return idx, nil
}

func (t *terminal) Input(ctx context.Context, message, help string) (string, error) {
	var answer string
	err := t.run(ctx, huh.NewInput().Title(message).Description(help).Value(&answer), false)
	return answer, err
}

func (t *terminal) Password(ctx context.Context, message string) (string, error) {
	var answer string
	err := t.run(ctx, huh.NewInput().Title(message).EchoMode(huh.EchoModeNone).Value(&answer), true)
	return answer, err
}
