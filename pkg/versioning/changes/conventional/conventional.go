// Package conventional parses conventional commit messages with the grammar of the Rust crate
// git-conventional (v1.1.0), which Knope uses. The grammar is copied exactly, quirks included, so that
// the same commit messages produce the same changes as in Knope. See the notes on [Parse].
//
// The package is pure: it takes a string and returns values.
package conventional

import (
	"strings"
	"unicode"
)

const (
	breakingPhrase = "BREAKING CHANGE"
	breakingArrow  = "BREAKING-CHANGE"

	// SeparatorValue is the footer separator ":" (the value follows, after optional whitespace).
	SeparatorValue = ":"
	// SeparatorRef is the footer separator " #" (the value is a reference, such as an issue number).
	SeparatorRef = " #"
)

// Commit is a parsed conventional commit.
type Commit struct {
	// Type is the commit type as written (compare it with [EqualFold]).
	Type string
	// Scope is the scope as written, or "" when absent. A scope is never empty when present.
	Scope string
	// HasScope reports whether the summary had a scope.
	HasScope bool
	// Breaking is the crate's breaking(): the summary had a "!" or a footer whose token is exactly
	// "BREAKING CHANGE" or "BREAKING-CHANGE" (case-sensitive here, unlike [Footer.IsBreaking]).
	Breaking bool
	// Exclamation reports whether the summary had a "!" before the colon.
	Exclamation bool
	// Description is the rest of the first line, after the colon and any whitespace.
	Description string
	// Body is the text between the summary and the footers, with trailing whitespace trimmed.
	Body string
	// HasBody distinguishes an absent body from an empty one (a body made only of whitespace, which the crate
	// reports as Some("")).
	HasBody bool
	// Footers are the footers in order, or nil.
	Footers []Footer
}

// Footer is one footer of a commit.
type Footer struct {
	// Token is the footer token as written.
	Token string
	// Separator is [SeparatorValue] or [SeparatorRef].
	Separator string
	// Value is the footer value, with continuation lines, trailing whitespace trimmed.
	Value string
}

// IsBreaking reports whether the token is BREAKING CHANGE or BREAKING-CHANGE, compared like the crate's
// FooterToken::breaking: case-insensitively, with full Unicode case folding ([EqualFold]).
func (f Footer) IsBreaking() bool {
	return EqualFold(f.Token, breakingPhrase) || EqualFold(f.Token, breakingArrow)
}

// Parse parses a commit message exactly like git-conventional's Commit::parse. It does not trim the message
// (Knope trims it with strings.TrimSpace before calling). The notable points of the grammar:
//
//   - Whitespace after the colon of the summary is Unicode whitespace including newlines, so "feat:\n\nx" has the
//     description "x".
//   - The line after the summary must be empty (or the input must end), else the error is [InvalidBody].
//   - A paragraph is a footer start when its first line begins with a token and a separator. Inside the body only
//     a line after a blank line starts the footers; inside a footer value any later line does. Everything after
//     the first footer is footers, even when a later paragraph is not footer-like (it becomes part of the previous
//     value).
//   - Every failure inside the summary is [MissingType], the crate's error kinds for scope and description are
//     never produced.
func Parse(message string) (Commit, error) {
	var c Commit
	rest, kind := parseSummary(message, &c)
	if kind != 0 {
		return Commit{}, &Error{Kind: kind, Commit: message}
	}

	// The body must start on the line after a blank line (or at the end of the input).
	var ok bool
	if rest, ok = cutLineEnding(rest); !ok && rest != "" {
		return Commit{}, &Error{Kind: InvalidBody, Commit: message}
	}
	rest = skipLineEndings(rest)

	if body, after, found := parseBody(rest); found {
		c.Body, c.HasBody, rest = body, true, after
	}

	for {
		footer, after, found, cut := parseFooter(rest)
		if cut {
			return Commit{}, &Error{Kind: InvalidFormat, Commit: message}
		}
		if !found {
			break
		}
		c.Footers = append(c.Footers, footer)
		rest = after
	}

	if rest = skipLineEndings(rest); rest != "" {
		return Commit{}, &Error{Kind: InvalidFormat, Commit: message}
	}

	c.Breaking = c.Exclamation
	for _, f := range c.Footers {
		if f.Token == breakingPhrase || f.Token == breakingArrow {
			c.Breaking = true
		}
	}
	return c, nil
}

// parseSummary parses the first line into c and the line ending after it. It returns the rest of the input, or
// a non-zero error kind.
func parseSummary(in string, c *Commit) (string, ErrorKind) {
	rest := in
	c.Type, rest = takeWhile(rest, isTypeRune)
	if c.Type == "" {
		return rest, MissingType
	}
	if strings.HasPrefix(rest, "(") {
		c.Scope, rest = takeWhile(rest[1:], isScopeRune)
		if c.Scope == "" || !strings.HasPrefix(rest, ")") {
			return rest, MissingType
		}
		c.HasScope = true
		rest = rest[1:]
	}
	if strings.HasPrefix(rest, "!") {
		c.Exclamation = true
		rest = rest[1:]
	}
	if !strings.HasPrefix(rest, ":") {
		return rest, MissingType
	}
	_, rest = takeWhile(rest[1:], unicode.IsSpace)
	c.Description, rest = takeWhile(rest, func(r rune) bool { return !isLineEnding(r) })
	if c.Description == "" {
		return rest, MissingType
	}
	rest, ok := cutLineEnding(rest)
	if !ok && rest != "" {
		return rest, InvalidFormat
	}
	return rest, 0
}

// parseBody takes lines until the first footer start that follows a blank line (or the start of the input).
func parseBody(in string) (body, rest string, found bool) {
	if in == "" {
		return "", in, false
	}
	offset := 0
	priorBlank := true
	for line := range linesWithTerminator(in) {
		if priorBlank && startsFooter(trimEnd(line)) {
			break
		}
		priorBlank = strings.TrimSpace(line) == ""
		offset += len(line)
	}
	if offset == 0 {
		return "", in, false
	}
	return trimEnd(in[:offset]), in[offset:], true
}

// parseFooter parses one footer. found is false when the input does not start with a token and a separator.
// cut is true when it does but the value is missing: the crate fails the whole parse then.
func parseFooter(in string) (footer Footer, rest string, found, cut bool) {
	token, rest, ok := cutToken(in)
	if !ok {
		return Footer{}, in, false, false
	}
	var sep string
	switch {
	case strings.HasPrefix(rest, SeparatorValue):
		sep = SeparatorValue
	case strings.HasPrefix(rest, SeparatorRef):
		sep = SeparatorRef
	default:
		return Footer{}, in, false, false
	}
	_, rest = takeWhile(rest[len(sep):], unicode.IsSpace)
	if rest == "" {
		return Footer{}, in, false, true
	}
	offset := 0
	first := true
	for line := range linesWithTerminator(rest) {
		if !first && startsFooter(trimEnd(line)) {
			break
		}
		first = false
		offset += len(line)
	}
	return Footer{Token: token, Separator: sep, Value: trimEnd(rest[:offset])}, rest[offset:], true, false
}

// startsFooter reports whether the line begins with a token followed by a separator.
func startsFooter(line string) bool {
	_, rest, ok := cutToken(line)
	return ok && (strings.HasPrefix(rest, SeparatorValue) || strings.HasPrefix(rest, SeparatorRef))
}

// cutToken takes a footer token: the literal "BREAKING CHANGE" (case-sensitive) or a type.
func cutToken(in string) (token, rest string, ok bool) {
	if strings.HasPrefix(in, breakingPhrase) {
		return breakingPhrase, in[len(breakingPhrase):], true
	}
	token, rest = takeWhile(in, isTypeRune)
	return token, rest, token != ""
}

func isLineEnding(r rune) bool { return r == '\n' || r == '\r' }

func isParens(r rune) bool { return r == '(' || r == ')' }

func isTypeRune(r rune) bool {
	return !isLineEnding(r) && !isParens(r) && r != ':' && r != '!' && !unicode.IsSpace(r)
}

func isScopeRune(r rune) bool { return !isLineEnding(r) && !isParens(r) }

func trimEnd(s string) string { return strings.TrimRightFunc(s, unicode.IsSpace) }

// takeWhile splits in after the longest prefix of runes that satisfy f.
func takeWhile(in string, f func(rune) bool) (prefix, rest string) {
	for i, r := range in {
		if !f(r) {
			return in[:i], in[i:]
		}
	}
	return in, ""
}

// cutLineEnding consumes one "\n" or "\r\n".
func cutLineEnding(in string) (rest string, ok bool) {
	if rest, ok = strings.CutPrefix(in, "\n"); ok {
		return rest, true
	}
	return strings.CutPrefix(in, "\r\n")
}

func skipLineEndings(in string) string {
	for {
		rest, ok := cutLineEnding(in)
		if !ok {
			return in
		}
		in = rest
	}
}

// linesWithTerminator yields the lines of s split after each "\n", keeping the terminator.
func linesWithTerminator(s string) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		for s != "" {
			end := strings.IndexByte(s, '\n') + 1
			if end == 0 {
				end = len(s)
			}
			line := s[:end]
			s = s[end:]
			if !yield(line) {
				return
			}
		}
	}
}
