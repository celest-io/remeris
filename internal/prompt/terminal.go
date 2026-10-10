package prompt

import (
	"context"
	"errors"
	"io"
	"os"

	"charm.land/huh/v2"
	"golang.org/x/term"
)

var (
	// ErrNotTTY is the cause when the input is not a terminal. It is inquire's NotTTY message.
	ErrNotTTY = errors.New("The input device is not a TTY") //nolint:revive,staticcheck // Knope's exact wording.
	// ErrInterrupted is the cause when the user interrupts a prompt (Ctrl-C). It is inquire's
	// Interrupted message.
	ErrInterrupted = errors.New("Operation was interrupted by the user") //nolint:revive,staticcheck // Knope's wording.
	errNoOptions   = errors.New("there is nothing to choose from")
)

type terminal struct {
	in  io.Reader
	out io.Writer
}

// NewTerminal returns the interactive Prompter. Like Knope (inquire), it refuses to prompt when in is not a
// terminal: every method then returns Error(ErrNotTTY). Only an *os.File that is a terminal qualifies.
func NewTerminal(in io.Reader, out io.Writer) Prompter {
	return &terminal{in: in, out: out}
}

// ready fails with ErrNotTTY when the input is not a terminal.
func (t *terminal) ready() error {
	if f, ok := t.in.(*os.File); ok && term.IsTerminal(int(f.Fd())) { //nolint:gosec // file descriptors fit in an int.
		return nil
	}
	return Error(ErrNotTTY)
}

// runError maps the error of a finished form to the package's diagnostic. huh reports a cancelled or expired
// context as ErrTimeout (its program is killed), so the context's own error is the cause when ctx is done.
func runError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return Error(ctxErr)
	}
	if errors.Is(err, huh.ErrUserAborted) {
		return Error(ErrInterrupted)
	}
	return Error(err)
}

func (t *terminal) run(ctx context.Context, field huh.Field) error {
	if err := t.ready(); err != nil {
		return err
	}
	form := huh.NewForm(huh.NewGroup(field)).WithInput(t.in).WithOutput(t.out)
	if err := form.RunWithContext(ctx); err != nil {
		return runError(ctx, err)
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
	err := t.run(ctx, huh.NewSelect[int]().Title(message).Options(indexedOptions(options)...).Value(&idx))
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
	if err := t.run(ctx, field); err != nil {
		return nil, err
	}
	return idx, nil
}

func (t *terminal) Input(ctx context.Context, message, help string) (string, error) {
	var answer string
	err := t.run(ctx, huh.NewInput().Title(message).Description(help).Value(&answer))
	return answer, err
}

func (t *terminal) Password(ctx context.Context, message string) (string, error) {
	var answer string
	err := t.run(ctx, huh.NewInput().Title(message).EchoMode(huh.EchoModeNone).Value(&answer))
	return answer, err
}
