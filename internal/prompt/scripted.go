package prompt

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
)

// Kind names the method of a Prompter that asked a question.
type Kind string

// The kinds of question, one per Prompter method.
const (
	KindSelect      Kind = "select"
	KindMultiSelect Kind = "multiselect"
	KindInput       Kind = "input"
	KindPassword    Kind = "password"
)

// ErrScript is wrapped by every error Scripted returns for a script mismatch: no answer left, an unexpected
// message, or an answer out of range.
var ErrScript = errors.New("prompt script mismatch")

// Answer is one scripted reply. Set the field that matches the kind of question: Index for Select, Indexes for
// MultiSelect, Text for Input and Password. Message, when not empty, is the question text the answer expects;
// a different question fails the prompt. Err, when set, is returned instead of an answer (use it to script an
// interrupt: it is returned as given, so wrap it with Error if the code under test expects a diagnostic).
type Answer struct {
	Message string
	Index   int
	Indexes []int
	Text    string
	Err     error
}

// Asked records one question put to Scripted.
type Asked struct {
	Kind       Kind
	Message    string
	Options    []string // Select and MultiSelect
	Help       string   // Input
	AtLeastOne string   // MultiSelect
}

// Scripted is a Prompter for tests, usable from any package's tests. It replies with the queued answers in
// the order the questions come, records every question, and fails (with an error wrapping ErrScript) when it
// runs out of answers, when an answer's expected Message differs from the question, or when an answer is out
// of range. It is safe for concurrent use.
type Scripted struct {
	mu      sync.Mutex
	answers []Answer
	asked   []Asked
}

// NewScripted returns a Scripted that replies with answers, first to last.
func NewScripted(answers ...Answer) *Scripted {
	return &Scripted{answers: slices.Clone(answers)}
}

// Asked returns a copy of the questions asked so far, in order.
func (s *Scripted) Asked() []Asked {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.asked)
}

// Remaining returns how many queued answers have not been used.
func (s *Scripted) Remaining() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.answers)
}

func (s *Scripted) next(ctx context.Context, q Asked) (Answer, error) {
	if err := ctx.Err(); err != nil {
		return Answer{}, Error(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	q.Options = slices.Clone(q.Options)
	s.asked = append(s.asked, q)
	if len(s.answers) == 0 {
		return Answer{}, Error(fmt.Errorf("%w: no answer left for %s %q", ErrScript, q.Kind, q.Message))
	}
	a := s.answers[0]
	s.answers = s.answers[1:]
	if a.Message != "" && a.Message != q.Message {
		return Answer{}, Error(fmt.Errorf("%w: expected %q, asked %q", ErrScript, a.Message, q.Message))
	}
	return a, a.Err
}

// Select implements Prompter.
func (s *Scripted) Select(ctx context.Context, message string, options []string) (int, error) {
	a, err := s.next(ctx, Asked{Kind: KindSelect, Message: message, Options: options})
	if err != nil {
		return 0, err
	}
	if a.Index < 0 || a.Index >= len(options) {
		return 0, Error(fmt.Errorf("%w: index %d out of range for %q", ErrScript, a.Index, message))
	}
	return a.Index, nil
}

// MultiSelect implements Prompter. Like the real prompt, it refuses an empty answer with the atLeastOne text.
func (s *Scripted) MultiSelect(ctx context.Context, message string, options []string, atLeastOne string) ([]int, error) {
	a, err := s.next(ctx, Asked{Kind: KindMultiSelect, Message: message, Options: options, AtLeastOne: atLeastOne})
	if err != nil {
		return nil, err
	}
	if len(a.Indexes) == 0 {
		return nil, Error(fmt.Errorf("%w: %s", ErrScript, atLeastOne))
	}
	for _, i := range a.Indexes {
		if i < 0 || i >= len(options) {
			return nil, Error(fmt.Errorf("%w: index %d out of range for %q", ErrScript, i, message))
		}
	}
	// The real prompt returns the chosen indexes in option order, once each.
	chosen := slices.Clone(a.Indexes)
	slices.Sort(chosen)
	return slices.Compact(chosen), nil
}

// Input implements Prompter.
func (s *Scripted) Input(ctx context.Context, message, help string) (string, error) {
	a, err := s.next(ctx, Asked{Kind: KindInput, Message: message, Help: help})
	return a.Text, err
}

// Password implements Prompter.
func (s *Scripted) Password(ctx context.Context, message string) (string, error) {
	a, err := s.next(ctx, Asked{Kind: KindPassword, Message: message})
	return a.Text, err
}
