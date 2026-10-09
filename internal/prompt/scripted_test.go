package prompt_test

import (
	"context"
	"errors"
	"testing"

	"github.com/celest-io/remeris/internal/prompt"
)

const (
	msgPackages = "Which packages does this change affect?"
	msgSummary  = "What is a short summary of this change?"
	helpSummary = "This will be used as a header in the changelog"
	msgAtLeast  = "At least one package must be selected"
)

var abc = []string{"a", "b", "c"}

func TestScriptedAnswers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := prompt.NewScripted(
		prompt.Answer{Indexes: []int{0, 2}},
		prompt.Answer{Message: "What type of change is this?", Index: 1},
		prompt.Answer{Message: msgSummary, Text: "add a thing"},
		prompt.Answer{Text: "s3cret"},
	)
	multi, err := s.MultiSelect(ctx, msgPackages, abc, msgAtLeast)
	if err != nil || len(multi) != 2 || multi[0] != 0 || multi[1] != 2 {
		t.Fatalf("MultiSelect = %v, %v", multi, err)
	}
	if idx, err := s.Select(ctx, "What type of change is this?", abc); err != nil || idx != 1 {
		t.Fatalf("Select = %d, %v", idx, err)
	}
	if text, err := s.Input(ctx, msgSummary, helpSummary); err != nil || text != "add a thing" {
		t.Fatalf("Input = %q, %v", text, err)
	}
	if text, err := s.Password(ctx, "Token"); err != nil || text != "s3cret" {
		t.Fatalf("Password = %q, %v", text, err)
	}
	if s.Remaining() != 0 {
		t.Errorf("Remaining = %d", s.Remaining())
	}
	asked := s.Asked()
	if len(asked) != 4 {
		t.Fatalf("asked %d questions", len(asked))
	}
	if a := asked[0]; a.Kind != prompt.KindMultiSelect || a.Message != msgPackages || a.AtLeastOne != msgAtLeast ||
		len(a.Options) != 3 {
		t.Errorf("asked[0] = %+v", a)
	}
	if a := asked[2]; a.Kind != prompt.KindInput || a.Help != helpSummary {
		t.Errorf("asked[2] = %+v", a)
	}
	if asked[3].Kind != prompt.KindPassword {
		t.Errorf("asked[3] = %+v", asked[3])
	}
}

func TestScriptedFailures(t *testing.T) {
	t.Parallel()
	errBoom := errors.New("boom")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := map[string]struct {
		answers []prompt.Answer
		ctx     context.Context
		ask     func(context.Context, prompt.Prompter) error
		is      error
	}{
		"out of answers": {
			ask: func(ctx context.Context, p prompt.Prompter) error { _, err := p.Input(ctx, "q", ""); return err },
			is:  prompt.ErrScript,
		},
		"unexpected message": {
			answers: []prompt.Answer{{Message: "expected", Text: "x"}},
			ask:     func(ctx context.Context, p prompt.Prompter) error { _, err := p.Input(ctx, "other", ""); return err },
			is:      prompt.ErrScript,
		},
		"select index out of range": {
			answers: []prompt.Answer{{Index: 3}},
			ask:     func(ctx context.Context, p prompt.Prompter) error { _, err := p.Select(ctx, "q", abc); return err },
			is:      prompt.ErrScript,
		},
		"multiselect empty": {
			answers: []prompt.Answer{{}},
			ask: func(ctx context.Context, p prompt.Prompter) error {
				_, err := p.MultiSelect(ctx, "q", abc, msgAtLeast)
				return err
			},
			is: prompt.ErrScript,
		},
		"multiselect index out of range": {
			answers: []prompt.Answer{{Indexes: []int{-1}}},
			ask: func(ctx context.Context, p prompt.Prompter) error {
				_, err := p.MultiSelect(ctx, "q", abc, msgAtLeast)
				return err
			},
			is: prompt.ErrScript,
		},
		"scripted error": {
			answers: []prompt.Answer{{Err: errBoom}},
			ask:     func(ctx context.Context, p prompt.Prompter) error { _, err := p.Password(ctx, "q"); return err },
			is:      errBoom,
		},
		"cancelled context": {
			ctx:     cancelled,
			answers: []prompt.Answer{{Text: "x"}},
			ask:     func(ctx context.Context, p prompt.Prompter) error { _, err := p.Input(ctx, "q", ""); return err },
			is:      context.Canceled,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := tc.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			err := tc.ask(ctx, prompt.NewScripted(tc.answers...))
			if !errors.Is(err, tc.is) {
				t.Fatalf("err = %v, want it to wrap %v", err, tc.is)
			}
		})
	}
}
