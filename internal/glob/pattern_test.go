package glob

import (
	"errors"
	"strings"
	"testing"
)

const (
	aUnderscoreB = "a_b"
	abcSlashDef  = "abc/def"
)

// The cases below are ported from the #[test] functions of Rust's glob crate
// (src/lib.rs, mod test), limited to the default match options.

func TestPatternMatches(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		pattern string
		yes     []string
		no      []string
	}{
		"wildcards a*b":   {pattern: "a*b", yes: []string{aUnderscoreB}},
		"wildcards a*b*c": {pattern: "a*b*c", yes: []string{"abc", "a_b_c", "a___b___c"}, no: []string{"abcd"}},
		"wildcards repeated": {
			pattern: "abc*abc*abc",
			yes:     []string{"abcabcabcabcabcabcabc"},
			no:      []string{"abcabcabcabcabcabcabca"},
		},
		"wildcards many a": {
			pattern: "a*a*a*a*a*a*a*a*a",
			yes:     []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		},
		"wildcards with set": {pattern: "a*b[xyz]c*d", yes: []string{"abxcdbxcddd"}},
		"recursive middle": {
			pattern: "some/**/needle.txt",
			yes:     []string{"some/needle.txt", "some/one/needle.txt", "some/one/two/needle.txt", "some/other/needle.txt"},
			no:      []string{"some/other/notthis.txt"},
		},
		"recursive alone": {pattern: "**", yes: []string{"abcde", "", ".asdf", "/x/.asdf"}},
		"recursive collapsed": {
			pattern: "some/**/**/needle.txt",
			yes:     []string{"some/needle.txt", "some/one/needle.txt", "some/one/two/needle.txt", "some/other/needle.txt"},
			no:      []string{"some/other/notthis.txt"},
		},
		"recursive first": {pattern: "**/test", yes: []string{"one/two/test", "one/test", "test"}},
		"recursive rooted": {
			pattern: "/**/test",
			yes:     []string{"/one/two/test", "/one/test", "/test"},
			no:      []string{"/one/notthis", "/notthis"},
		},
		"recursive dotfile": {pattern: "**/.*", yes: []string{".abc", "abc/.abc"}, no: []string{"ab.c", "abc/ab.c"}},
		"range digits": {
			pattern: "a[0-9]b",
			yes:     []string{"a0b", "a1b", "a2b", "a3b", "a4b", "a5b", "a6b", "a7b", "a8b", "a9b"},
			no:      []string{aUnderscoreB},
		},
		"range negated": {
			pattern: "a[!0-9]b",
			yes:     []string{aUnderscoreB},
			no:      []string{"a0b", "a5b", "a9b"},
		},
		"range then singles": {pattern: "[a-z123]", yes: []string{"a", "m", "z", "1", "2", "3"}, no: []string{"A", "4"}},
		"singles then range": {pattern: "[1a-z23]", yes: []string{"a", "z", "1", "2", "3"}, no: []string{"A"}},
		"singles before":     {pattern: "[123a-z]", yes: []string{"a", "z", "1", "2", "3"}, no: []string{"A"}},
		"trailing dash":      {pattern: "[abc-]", yes: []string{"a", "b", "c", "-"}, no: []string{"d"}},
		"leading dash":       {pattern: "[-abc]", yes: []string{"a", "b", "c", "-"}, no: []string{"d"}},
		"range then dash":    {pattern: "[a-c-]", yes: []string{"a", "b", "c", "-"}, no: []string{"d"}},
		"empty range":        {pattern: "[2-1]", no: []string{"1", "2"}},
		"only dash":          {pattern: "[-]", yes: []string{"-"}},
		"not dash":           {pattern: "[!-]", no: []string{"-"}},
		"star crosses slash": {
			pattern: "*hello.txt",
			yes: []string{
				"hello.txt",
				"gareth_says_hello.txt",
				"some/path/to/hello.txt",
				`some\path\to\hello.txt`,
				"/an/absolute/path/to/hello.txt",
			},
			no: []string{"hello.txt-and-then-some", "goodbye.txt"},
		},
		"star then directory": {
			pattern: "*some/path/to/hello.txt",
			yes:     []string{"some/path/to/hello.txt", "a/bigger/some/path/to/hello.txt"},
			no:      []string{"some/path/to/hello.txt-and-then-some", "some/other/path/to/hello.txt"},
		},
		"escaped by brackets": {pattern: "_[[]_[]]_[?]_[*]_!_", yes: []string{"_[_]_?_*_!_"}},
		"literal slash":       {pattern: abcSlashDef, yes: []string{abcSlashDef}},
		"question matches /":  {pattern: "abc?def", yes: []string{abcSlashDef}},
		"star matches /":      {pattern: "abc*def", yes: []string{abcSlashDef}},
		"set matches /":       {pattern: "abc[/]def", yes: []string{abcSlashDef}},
		"star matches dot":    {pattern: "*.txt", yes: []string{".hello.txt"}},
		"dot star star":       {pattern: ".*.*", yes: []string{".hello.txt"}},
		"dir star dotfile":    {pattern: "aaa/bbb/*", yes: []string{"aaa/bbb/.ccc", "aaa/bbb/c.c.c."}},
		"dir dot star":        {pattern: "aaa/bbb/.*", yes: []string{"aaa/bbb/.ccc"}},
		"question dot":        {pattern: "aaa/?bbb", yes: []string{"aaa/.bbb"}},
		"set dot":             {pattern: "aaa/[.]bbb", yes: []string{"aaa/.bbb"}},
		"recursive star dot":  {pattern: "**/*", yes: []string{".bbb"}},
		"case sensitive":      {pattern: "aBcDeFg", yes: []string{"aBcDeFg"}, no: []string{"abcdefg", "ABCDEFG"}},
		"case sensitive set":  {pattern: "[a]", yes: []string{"a"}, no: []string{"A"}},
		"case sensitive neg":  {pattern: "[!a]", yes: []string{"A"}, no: []string{"a"}},
		"match path":          {pattern: pathAB, yes: []string{pathAB}},
		"closing bracket":     {pattern: "[]]", yes: []string{"]"}, no: []string{"a"}},
		"negated closing":     {pattern: "[!]]", yes: []string{"a"}, no: []string{"]"}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p, err := compile(tc.pattern)
			if err != nil {
				t.Fatalf("compile(%q): %v", tc.pattern, err)
			}
			for _, s := range tc.yes {
				if !p.matches(s) {
					t.Errorf("%q should match %q", tc.pattern, s)
				}
			}
			for _, s := range tc.no {
				if p.matches(s) {
					t.Errorf("%q should not match %q", tc.pattern, s)
				}
			}
		})
	}
}

func TestPatternRecursiveFlag(t *testing.T) {
	t.Parallel()
	p, err := compile("**")
	if err != nil || !p.recursive {
		t.Fatalf("compile(**) = %+v, %v; want a recursive pattern", p, err)
	}
	if p, err = compile("a/*"); err != nil || p.recursive {
		t.Fatalf("compile(a/*) = %+v, %v; want a plain pattern", p, err)
	}
}

func TestPatternErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		pattern string
		pos     int
		msg     string
	}{
		"double star then letter":   {"a/**b", 4, errRecursiveWildcards},
		"letters then double star":  {"a/bc**", 3, errRecursiveWildcards},
		"five stars":                {"a/*****", 4, errWildcards},
		"embedded double stars":     {"a/b**c**d", 2, errRecursiveWildcards},
		"letter then double star":   {"a**b", 0, errRecursiveWildcards},
		"unclosed bracket":          {"abc[def", 3, errInvalidRange},
		"unclosed negated":          {"abc[!def", 3, errInvalidRange},
		"bracket at end":            {"abc[", 3, errInvalidRange},
		"negation at end":           {"abc[!", 3, errInvalidRange},
		"one char unclosed":         {"abc[d", 3, errInvalidRange},
		"one char unclosed negated": {"abc[!d", 3, errInvalidRange},
		"empty set":                 {"abc[]", 3, errInvalidRange},
		"empty negated set":         {"abc[!]", 3, errInvalidRange},
		"unicode shifts by chars":   {"é/é[", 3, errInvalidRange},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := compile(tc.pattern)
			var perr *PatternError
			if !errors.As(err, &perr) {
				t.Fatalf("compile(%q) error = %v, want a *PatternError", tc.pattern, err)
			}
			if perr.Pos != tc.pos || perr.Msg != tc.msg {
				t.Errorf("compile(%q) = {%d, %q}, want {%d, %q}", tc.pattern, perr.Pos, perr.Msg, tc.pos, tc.msg)
			}
			if !strings.HasPrefix(perr.Error(), "Pattern syntax error near position ") {
				t.Errorf("Error() = %q", perr.Error())
			}
		})
	}
}
