// Package command splits command lines into arguments for the Command step (spec F5, 0.3).
package command

import (
	"strings"
	"unicode"
)

// Split breaks a command line into words, the way Knope's `execute` crate does:
//
//   - whitespace separates words;
//   - '…' and "…" group characters, including whitespace, into one word, and the other kind of
//     quote is an ordinary character inside them;
//   - a backslash makes the next character literal, inside quotes too;
//   - quoted and unquoted parts that touch form a single word, and "" is an empty word;
//   - nothing else is special: in particular $VAR is never expanded. That is what shell = true is for.
//
// An unterminated quote runs to the end of the line and a trailing backslash is dropped.
func Split(line string) []string {
	var (
		words    []string
		word     strings.Builder
		inWord   bool // a word is being built, even if still empty (as with "")
		quote    rune // the open quote character, or 0
		escaping bool
	)
	flush := func() {
		if inWord {
			words = append(words, word.String())
			word.Reset()
			inWord = false
		}
	}
	for _, r := range line {
		switch {
		case escaping:
			escaping = false
			word.WriteRune(r)
			inWord = true
		case r == '\\':
			escaping = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				word.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
			inWord = true
		case unicode.IsSpace(r):
			flush()
		default:
			word.WriteRune(r)
			inWord = true
		}
	}
	flush()
	return words
}
