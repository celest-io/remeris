package glob

import "fmt"

type tokenKind uint8

const (
	tokChar      tokenKind = iota // a literal rune
	tokAny                        // ?
	tokSeq                        // *
	tokRecursive                  // ** as a whole path component
	tokWithin                     // [abc] or [a-z]
	tokExcept                     // [!abc]
)

const (
	errWildcards          = "wildcards are either regular `*` or recursive `**`"
	errRecursiveWildcards = "recursive wildcards must form a single path component"
	errInvalidRange       = "invalid range pattern"
)

// PatternError reports a syntax error in a pattern. Pos is the approximate
// index (in characters) where the error occurred.
type PatternError struct {
	Pos int
	Msg string
}

func (e *PatternError) Error() string {
	return fmt.Sprintf("Pattern syntax error near position %d: %s", e.Pos, e.Msg)
}

type span struct{ lo, hi rune }

type token struct {
	kind tokenKind
	char rune
	set  []span
}

// pattern is one compiled pattern (a whole glob or one of its path components).
type pattern struct {
	src       string
	tokens    []token
	recursive bool // contains a ** token
	meta      bool // contains any metacharacter
}

// compile ports Pattern::new of the Rust glob crate.
func compile(src string) (*pattern, error) {
	chars := []rune(src)
	p := &pattern{src: src}
	for i := 0; i < len(chars); {
		var err error
		switch chars[i] {
		case '?':
			p.meta = true
			p.tokens = append(p.tokens, token{kind: tokAny})
			i++
		case '*':
			p.meta = true
			i, err = p.star(chars, i)
		case '[':
			p.meta = true
			i, err = p.bracket(chars, i)
		default:
			p.tokens = append(p.tokens, token{kind: tokChar, char: chars[i]})
			i++
		}
		if err != nil {
			return nil, err
		}
	}
	return p, nil
}

// star parses a run of * starting at old and returns the next index.
func (p *pattern) star(chars []rune, old int) (int, error) {
	i := old
	for i < len(chars) && chars[i] == '*' {
		i++
	}
	switch count := i - old; {
	case count > 2:
		return 0, &PatternError{Pos: old + 2, Msg: errWildcards}
	case count < 2:
		p.tokens = append(p.tokens, token{kind: tokSeq})
		return i, nil
	}
	// ** must be a whole path component: a/**/b is valid, a**/b and a/**b are not.
	if i != 2 && chars[old-1] != '/' {
		return 0, &PatternError{Pos: old - 1, Msg: errRecursiveWildcards}
	}
	switch {
	case i < len(chars) && chars[i] == '/':
		i++
	case i != len(chars):
		return 0, &PatternError{Pos: i, Msg: errRecursiveWildcards}
	}
	if n := len(p.tokens); n <= 1 || p.tokens[n-1].kind != tokRecursive {
		p.recursive = true
		p.tokens = append(p.tokens, token{kind: tokRecursive})
	}
	return i, nil
}

// bracket parses [...] or [!...] starting at i and returns the next index.
// A ] right after [ or [! is part of the set.
func (p *pattern) bracket(chars []rune, i int) (int, error) {
	kind, from := tokWithin, i+1
	if i+4 <= len(chars) && chars[i+1] == '!' {
		kind, from = tokExcept, i+2
	} else if i+3 > len(chars) || chars[i+1] == '!' {
		return 0, &PatternError{Pos: i, Msg: errInvalidRange}
	}
	for j := from + 1; j < len(chars); j++ {
		if chars[j] == ']' {
			p.tokens = append(p.tokens, token{kind: kind, set: parseSpans(chars[from:j])})
			return j + 1, nil
		}
	}
	return 0, &PatternError{Pos: i, Msg: errInvalidRange}
}

func parseSpans(s []rune) []span {
	var out []span
	for i := 0; i < len(s); {
		if i+3 <= len(s) && s[i+1] == '-' {
			out = append(out, span{s[i], s[i+2]})
			i += 3
		} else {
			out = append(out, span{s[i], s[i]})
			i++
		}
	}
	return out
}

func inSet(set []span, c rune) bool {
	for _, s := range set {
		if c >= s.lo && c <= s.hi {
			return true
		}
	}
	return false
}

// matches reports whether s matches the whole pattern, with the crate's default
// options: case sensitive, * and ? also match /, dotfiles are not special.
func (p *pattern) matches(s string) bool {
	return matchFrom(p.tokens, []rune(s))
}

func matchFrom(tokens []token, s []rune) bool {
	for ti, t := range tokens {
		if t.kind == tokSeq || t.kind == tokRecursive {
			for i := 0; i <= len(s); i++ {
				// ** only resumes at the start, after a separator, or at the end.
				if t.kind == tokRecursive && i > 0 && i < len(s) && s[i-1] != '/' {
					continue
				}
				if matchFrom(tokens[ti+1:], s[i:]) {
					return true
				}
			}
			return false
		}
		if len(s) == 0 || !t.matchesRune(s[0]) {
			return false
		}
		s = s[1:]
	}
	return len(s) == 0
}

func (t token) matchesRune(c rune) bool {
	switch t.kind {
	case tokAny:
		return true
	case tokWithin:
		return inSet(t.set, c)
	case tokExcept:
		return !inSet(t.set, c)
	default:
		return c == t.char
	}
}
