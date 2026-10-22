package denosemver

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// This file is a small port of the parser combinators of the monch crate 0.5.0 (src/lib.rs, MIT licence,
// Copyright 2018-2024 the Deno authors), which deno_semver 0.9.1 builds its grammars on. Only the combinators the
// grammars use are ported. The distinction between a backtrace (try the next alternative) and a failure (stop
// with a message) decides how some inputs parse, so it is kept as it is.

// parseErr is monch's ParseError. A nil *parseErr means success; a parseErr with failure unset is a backtrace.
type parseErr struct {
	failure bool
	input   string
	message string
}

var errBacktrace = &parseErr{}

func fail(input, message string) *parseErr {
	return &parseErr{failure: true, input: input, message: message}
}

func isBacktrace(e *parseErr) bool { return e != nil && !e.failure }

// parser is monch's combinator shape: it returns the remaining input and a value, or a *parseErr.
type parser[T any] = func(in string) (string, T, *parseErr)

type tuple[A, B any] struct {
	first  A
	second B
}

// option is Rust's Option for the values that maybe returns.
type option[T any] struct {
	val T
	ok  bool
}

// Failure is monch's ParseErrorFailureError: what a grammar reported and the start of the input it stopped at.
type Failure struct {
	// Message says what is wrong.
	Message string
	// CodeSnippet holds at most the first 60 characters of the input where the parser stopped.
	CodeSnippet string
}

// Error returns the message, the snippet and a marker line, like the Rust Display implementation.
func (f *Failure) Error() string {
	return f.Message + "\n  " + f.CodeSnippet + "\n  ~"
}

func newFailure(input, message string) *Failure {
	snippet := input
	n := 0
	for i := range input {
		if n == 60 {
			snippet = input[:i]
			break
		}
		n++
	}
	return &Failure{Message: message, CodeSnippet: snippet}
}

// withFailureHandling runs the parser and requires it to consume the whole input (monch with_failure_handling).
func withFailureHandling[T any](p parser[T]) func(string) (T, *Failure) {
	return func(input string) (T, *Failure) {
		rest, v, err := p(input)
		var zero T
		switch {
		case err == nil && rest != "":
			return zero, newFailure(rest, "Unexpected character.")
		case err == nil:
			return v, nil
		case !err.failure:
			return zero, newFailure(input, "Unexpected character.")
		default:
			return zero, newFailure(err.input, err.message)
		}
	}
}

func nextChar(in string) (string, rune, *parseErr) {
	if in == "" {
		return in, 0, errBacktrace
	}
	r, size := utf8.DecodeRuneInString(in)
	return in[size:], r, nil
}

func ifTrue[T any](p parser[T], cond func(T) bool) parser[T] {
	return func(in string) (string, T, *parseErr) {
		rest, v, err := p(in)
		if err != nil {
			return in, v, err
		}
		if !cond(v) {
			return in, v, errBacktrace
		}
		return rest, v, nil
	}
}

func ch(c rune) parser[rune] {
	return ifTrue(nextChar, func(found rune) bool { return found == c })
}

func tag(value string) parser[string] {
	return func(in string) (string, string, *parseErr) {
		if strings.HasPrefix(in, value) {
			return in[len(value):], in[:len(value)], nil
		}
		return in, "", errBacktrace
	}
}

func substring[T any](p parser[T]) parser[string] {
	return func(in string) (string, string, *parseErr) {
		rest, _, err := p(in)
		if err != nil {
			return in, "", err
		}
		return rest, in[:len(in)-len(rest)], nil
	}
}

func skipWhile(cond func(rune) bool) parser[struct{}] {
	return func(in string) (string, struct{}, *parseErr) {
		for pos, c := range in {
			if !cond(c) {
				return in[pos:], struct{}{}, nil
			}
		}
		return "", struct{}{}, nil
	}
}

func takeWhile(cond func(rune) bool) parser[string] { return substring(skipWhile(cond)) }

func maybe[T any](p parser[T]) parser[option[T]] {
	return func(in string) (string, option[T], *parseErr) {
		rest, v, err := p(in)
		switch {
		case err == nil:
			return rest, option[T]{val: v, ok: true}, nil
		case isBacktrace(err):
			return in, option[T]{}, nil
		default:
			return in, option[T]{}, err
		}
	}
}

func mapP[A, B any](p parser[A], f func(A) B) parser[B] {
	return func(in string) (string, B, *parseErr) {
		rest, v, err := p(in)
		if err != nil {
			var zero B
			return in, zero, err
		}
		return rest, f(v), nil
	}
}

// or tries the alternatives in order; only a backtrace moves on to the next one.
func or[T any](a, b parser[T], more ...parser[T]) parser[T] {
	return func(in string) (string, T, *parseErr) {
		rest, v, err := a(in)
		if !isBacktrace(err) {
			return rest, v, err
		}
		if len(more) == 0 {
			return b(in)
		}
		return or(b, more[0], more[1:]...)(in)
	}
}

func pair[A, B any](a parser[A], b parser[B]) parser[tuple[A, B]] {
	return func(in string) (string, tuple[A, B], *parseErr) {
		rest, first, err := a(in)
		if err != nil {
			return in, tuple[A, B]{}, err
		}
		rest, second, err := b(rest)
		if err != nil {
			return in, tuple[A, B]{}, err
		}
		return rest, tuple[A, B]{first: first, second: second}, nil
	}
}

func preceded[A, B any](a parser[A], b parser[B]) parser[B] {
	return mapP(pair(a, b), func(t tuple[A, B]) B { return t.second })
}

func terminated[A, B any](a parser[A], b parser[B]) parser[A] {
	return mapP(pair(a, b), func(t tuple[A, B]) A { return t.first })
}

func delimited[A, B, C any](a parser[A], b parser[B], c parser[C]) parser[B] {
	return func(in string) (string, B, *parseErr) {
		var zero B
		rest, _, err := a(in)
		if err != nil {
			return in, zero, err
		}
		rest, v, err := b(rest)
		if err != nil {
			return in, zero, err
		}
		rest, _, err = c(rest)
		if err != nil {
			return in, zero, err
		}
		return rest, v, nil
	}
}

// separatedList keeps parsing items separated by sep. A trailing separator is accepted, and a failing item
// ends the list without an error.
func separatedList[O, S any](p parser[O], sep parser[S]) parser[[]O] {
	return func(in string) (string, []O, *parseErr) {
		var results []O
		input := in
		for input != "" {
			rest, v, err := p(input)
			if isBacktrace(err) {
				return input, results, nil
			}
			if err != nil {
				return in, nil, err
			}
			results = append(results, v)
			input = rest
			rest, _, err = sep(input)
			if isBacktrace(err) {
				break
			}
			if err != nil {
				return in, nil, err
			}
			input = rest
		}
		return input, results, nil
	}
}

// whitespace expects at least one whitespace character (Rust char::is_whitespace, which is White_Space).
func whitespace(in string) (string, string, *parseErr) {
	if in == "" {
		return in, "", errBacktrace
	}
	for pos, c := range in {
		if !unicode.IsSpace(c) {
			if pos == 0 {
				return in, "", errBacktrace
			}
			return in[pos:], in[:pos], nil
		}
	}
	return "", in, nil
}

func skipWhitespace(in string) (string, struct{}, *parseErr) {
	rest, _, err := whitespace(in)
	if err == nil {
		return rest, struct{}{}, nil
	}
	if isBacktrace(err) {
		return in, struct{}{}, nil
	}
	return in, struct{}{}, err
}

func isASCIIDigit(c rune) bool { return c >= '0' && c <= '9' }
