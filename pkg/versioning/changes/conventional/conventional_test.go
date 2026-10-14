package conventional

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// The cases of this file are ported from the git-conventional crate v1.1.0 (src/commit.rs, src/parser.rs,
// src/lib.rs, README.md). Cases marked "oracle" were added after running the crate on them.

const (
	typeFeat       = "feat"
	typeFix        = "fix"
	helloWorld     = "hello world"
	breakingChange = "breaking change"
	somethingBroke = "something broke"
	bodyText       = "body"
	closesToken    = "closes"
	// Rust's indoc! strips the common indent, so these are the dedented texts of the crate's tests.
	fakeFooterBody = "First line of the body\nIMPORTANT: Please see something else for details.\nAnother line here."
)

const (
	typeWord    = "type"
	foo         = "foo"
	bar         = "bar"
	baz         = "baz"
	empty       = "empty"
	spacedFeat  = " feat "
	lowercase   = "lowercase"
	capFoo      = "Foo"
	upperFoo    = "FOO"
	mixedFoo    = "fOO"
	dashedFoo   = "foo-bar"
	fooBar      = "foo bar"
	fooColonBar = "foo: bar"
	twoSpaces   = "  "
	spaceName   = "space"
)

func TestParseValid(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		message string
		want    Commit
	}{
		"valid simple commit": {
			message: "type(my scope): hello world",
			want:    Commit{Type: typeWord, Scope: "my scope", HasScope: true, Description: helloWorld},
		},
		"trailing whitespace without body": {
			message: "type(my scope): hello world\n\n\n",
			want:    Commit{Type: typeWord, Scope: "my scope", HasScope: true, Description: helloWorld},
		},
		"trailing 1 newline": {
			message: "type: hello world\n",
			want:    Commit{Type: typeWord, Description: helloWorld},
		},
		"trailing 2 newlines": {
			message: "type: hello world\n\n",
			want:    Commit{Type: typeWord, Description: helloWorld},
		},
		"trailing 3 newlines": {
			message: "type: hello world\n\n\n",
			want:    Commit{Type: typeWord, Description: helloWorld},
		},
		"parenthetical statement": {
			message: "type: hello world (#1)",
			want:    Commit{Type: typeWord, Description: "hello world (#1)"},
		},
		"issue 12 case 1": {
			message: "chore: add .hello.txt (#1)\n\n",
			want:    Commit{Type: "chore", Description: "add .hello.txt (#1)"},
		},
		"issue 12 case 2": {
			message: "refactor: use fewer lines (#3)\n\n",
			want:    Commit{Type: "refactor", Description: "use fewer lines (#3)"},
		},
		"breaking exclamation mark": {
			message: "feat!: this is a breaking change",
			want:    Commit{Type: typeFeat, Breaking: true, Exclamation: true, Description: "this is a breaking change"},
		},
		"breaking phrase footer": {
			message: "feat: message\n\nBREAKING CHANGE: breaking change",
			want: Commit{
				Type: typeFeat, Breaking: true, Description: "message",
				Footers: []Footer{{breakingPhrase, SeparatorValue, breakingChange}},
			},
		},
		"breaking arrow footer": {
			message: "fix: message\n\nBREAKING-CHANGE: it's broken",
			want: Commit{
				Type: typeFix, Breaking: true, Description: "message",
				Footers: []Footer{{breakingArrow, SeparatorValue, "it's broken"}},
			},
		},
		"conjoined footer": {
			message: "fix(example): fix keepachangelog config example\n\nFixes: #123, #124, #125",
			want: Commit{
				Type: typeFix, Scope: "example", HasScope: true, Description: "fix keepachangelog config example",
				Footers: []Footer{{"Fixes", SeparatorValue, "#123, #124, #125"}},
			},
		},
		"windows line endings": {
			message: "feat: thing\r\n\r\nbody\r\n\r\ncloses #1234\r\n\r\n\r\nBREAKING CHANGE: something broke\r\n\r\n",
			want: Commit{
				Type: typeFeat, Breaking: true, Description: "thing", Body: bodyText, HasBody: true,
				Footers: []Footer{{closesToken, SeparatorRef, "1234"}, {breakingPhrase, SeparatorValue, somethingBroke}},
			},
		},
		"extra line endings": {
			message: "feat: thing\n\n\n\n\nbody\n\n\n\n\ncloses #1234\n\n\n\n\n\nBREAKING CHANGE: something broke\n\n\n\n",
			want: Commit{
				Type: typeFeat, Breaking: true, Description: "thing", Body: bodyText, HasBody: true,
				Footers: []Footer{{closesToken, SeparatorRef, "1234"}, {breakingPhrase, SeparatorValue, somethingBroke}},
			},
		},
		"fake footer in the body": {
			message: "fix: something something\n\nFirst line of the body\nIMPORTANT: Please see something else for details.\n" +
				"Another line here.\n",
			want: Commit{Type: typeFix, Description: "something something", Body: fakeFooterBody, HasBody: true},
		},
		"valid complex commit": {
			message: "chore: improve changelog readability\n\n" +
				"Change date notation from YYYY-MM-DD to YYYY.MM.DD to make it a tiny bit\neasier to parse while reading.\n\n" +
				"BREAKING CHANGE: Just kidding!\n",
			want: Commit{
				Type:        "chore",
				Breaking:    true,
				Description: "improve changelog readability",
				HasBody:     true,
				Body:        "Change date notation from YYYY-MM-DD to YYYY.MM.DD to make it a tiny bit\neasier to parse while reading.",
				Footers:     []Footer{{breakingPhrase, SeparatorValue, "Just kidding!"}},
			},
		},
		"lib.rs documentation example": {
			message: "docs(example)!: add tested usage example\n\n" +
				"This example is tested using Rust's doctest capabilities. Having this\n" +
				"example helps people understand how to use the parser.\n\n" +
				"BREAKING CHANGE: Going from nothing to something, meaning anyone doing\n" +
				"nothing before suddenly has something to do. That sounds like a change\n" +
				"in your break.\n\n" +
				"Co-Authored-By: Lisa Simpson <lisa@simpsons.fam>\n" +
				"Closes #12\n",
			want: Commit{
				Type: "docs", Scope: "example", HasScope: true, Breaking: true, Exclamation: true,
				Description: "add tested usage example", HasBody: true,
				Body: "This example is tested using Rust's doctest capabilities. Having this\n" +
					"example helps people understand how to use the parser.",
				Footers: []Footer{
					{
						breakingPhrase, SeparatorValue,
						"Going from nothing to something, meaning anyone doing\nnothing before suddenly has something to do. " +
							"That sounds like a change\nin your break.",
					},
					{"Co-Authored-By", SeparatorValue, "Lisa Simpson <lisa@simpsons.fam>"},
					{"Closes", SeparatorRef, "12"},
				},
			},
		},
		"README example": {
			message: "feat(conventional commit): this is it!",
			want:    Commit{Type: typeFeat, Scope: "conventional commit", HasScope: true, Description: "this is it!"},
		},
		// oracle
		"no space after the colon": {
			message: "feat:x",
			want:    Commit{Type: typeFeat, Description: "x"},
		},
		"whitespace after the colon includes newlines": {
			message: "feat:\n\nx",
			want:    Commit{Type: typeFeat, Description: "x"},
		},
		"scope may contain a colon and a space": {
			message: "feat(a: b): x",
			want:    Commit{Type: typeFeat, Scope: "a: b", HasScope: true, Description: "x"},
		},
		"body of only whitespace is present and empty": {
			message: "feat: a\n\n  \n",
			want:    Commit{Type: typeFeat, Description: "a", HasBody: true},
		},
		"body with a footer-like line that is not after a blank line": {
			message: "feat: a\n\nsome text\nfoo: bar",
			want:    Commit{Type: typeFeat, Description: "a", Body: "some text\nfoo: bar", HasBody: true},
		},
		"footer after a blank line in the middle of the body": {
			message: "feat: a\n\ntext\n\nfoo: bar\n\nmore text",
			want: Commit{
				Type: typeFeat, Description: "a", Body: "text", HasBody: true,
				Footers: []Footer{{foo, SeparatorValue, "bar\n\nmore text"}},
			},
		},
		"footer value with continuation lines": {
			message: "feat: a\n\nfoo: bar\n  baz\nqux #1\nlast one",
			want: Commit{
				Type: typeFeat, Description: "a",
				Footers: []Footer{{foo, SeparatorValue, "bar\n  baz"}, {"qux", SeparatorRef, "1\nlast one"}},
			},
		},
		"footer value starts on the next line": {
			message: "feat: a\n\nfoo:\nbar",
			want:    Commit{Type: typeFeat, Description: "a", Footers: []Footer{{foo, SeparatorValue, bar}}},
		},
		"footer token is any type character": {
			message: "feat: a\n\nÉtiquette_1.x: v",
			want:    Commit{Type: typeFeat, Description: "a", Footers: []Footer{{"Étiquette_1.x", SeparatorValue, "v"}}},
		},
		"lowercase breaking arrow footer is not Commit.Breaking": {
			message: "feat: a\n\nbreaking-change: b",
			want:    Commit{Type: typeFeat, Description: "a", Footers: []Footer{{"breaking-change", SeparatorValue, "b"}}},
		},
		"lowercase breaking phrase is not a token": {
			message: "feat: a\n\nbreaking change: b",
			want:    Commit{Type: typeFeat, Description: "a", Body: "breaking change: b", HasBody: true},
		},
		"BREAKING CHANGE followed by more letters": {
			message: "feat: a\n\nBREAKING CHANGES: b",
			want:    Commit{Type: typeFeat, Description: "a", Body: "BREAKING CHANGES: b", HasBody: true},
		},
		"exclamation and footer": {
			message: "feat!: a\n\nBREAKING CHANGE: b",
			want: Commit{
				Type: typeFeat, Breaking: true, Exclamation: true, Description: "a",
				Footers: []Footer{{breakingPhrase, SeparatorValue, "b"}},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := Parse(tc.message)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tc.message, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Parse(%q)\n got: %+v\nwant: %+v", tc.message, got, tc.want)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		message string
		want    ErrorKind
	}{
		empty:           {"", MissingType},
		"missing type":  {"Hello World", MissingType},
		"missing colon": {"fix Improved error messages\n", MissingType},
		"multiline description": {
			"chore: Automate fastlane when a file in the fastlane directory is\nchanged (hopefully)",
			InvalidBody,
		},
		"only a type":                    {foo, MissingType},
		"space before the colon":         {"foo : bar", MissingType},
		"two words before the colon":     {"foo bar: baz", MissingType},
		"unclosed scope":                 {"foo(: bar", MissingType},
		"unopened scope":                 {"foo): bar", MissingType},
		"empty scope":                    {"foo(): bar", MissingType},
		"scope without colon":            {"foo(bar)", MissingType},
		"scope and empty description":    {"foo(bar):", MissingType},
		"scope and blank description":    {"foo(bar): ", MissingType},
		"scope and blanker description":  {"foo(bar):  ", MissingType},
		"space after the scope":          {"foo(bar) :baz", MissingType},
		"space before the scope":         {"foo (bar): baz", MissingType},
		"words before the scope":         {"foo bar(baz): qux", MissingType},
		"leading space":                  {" feat: a", MissingType},
		"leading space and type":         {spacedFeat, MissingType},
		"closing parenthesis":            {")", MissingType},
		"description with blank lines":   {"feat: \n\n", MissingType},
		"no-break space ends the type":   {"feat x: a", MissingType},
		"exclamation before the scope":   {"feat!(x): a", MissingType},
		"lone carriage return":           {"feat: a\rb", InvalidFormat},
		"footer without a value":         {"a: b\n\nfoo:", InvalidFormat},
		"footer with a blank value":      {"a: b\n\nfoo: ", InvalidFormat},
		"footer with a reference marker": {"a: b\n\nfoo #", InvalidFormat},
		"second footer without a value":  {"a: b\n\nfoo: a\nbar:", InvalidFormat},
		"text after the summary":         {"feat: a\ntext", InvalidBody},
		"text after a carriage return":   {"feat: a\r\ntext", InvalidBody},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse(tc.message)
			var parseErr *Error
			if !errors.As(err, &parseErr) {
				t.Fatalf("Parse(%q) error = %v, want *Error", tc.message, err)
			}
			if parseErr.Kind != tc.want {
				t.Errorf("Parse(%q) kind = %v (%d), want %v (%d)", tc.message, parseErr.Kind, parseErr.Kind, tc.want, tc.want)
			}
			if parseErr.Commit != tc.message || parseErr.Error() != tc.want.String() {
				t.Errorf("Parse(%q) error = %+v", tc.message, parseErr)
			}
		})
	}
}

func TestErrorKindMessages(t *testing.T) {
	t.Parallel()

	tests := map[ErrorKind]string{
		MissingType:        "Missing type in the commit summary, expected `type: description`",
		InvalidScope:       "Incorrect scope syntax in commit summary, expected `type(scope): description`",
		MissingDescription: "Missing description in commit summary, expected `type: description`",
		InvalidBody:        "Incorrect body syntax",
		InvalidFooter:      "Incorrect footer syntax",
		InvalidFormat:      "Incorrect conventional commit format",
		ErrorKind(0):       "Unknown conventional commit error",
	}
	for kind, want := range tests {
		if got := kind.String(); got != want {
			t.Errorf("ErrorKind(%d).String() = %q, want %q", kind, got, want)
		}
	}
}

// Ported from parser.rs: the type parser accepts a prefix.
func TestTypeGrammar(t *testing.T) {
	t.Parallel()

	tests := map[string]struct{ in, want, rest string }{
		lowercase:                {foo, foo, ""},
		"capitalized":            {capFoo, capFoo, ""},
		"all upper":              {upperFoo, upperFoo, ""},
		"mixed case":             {mixedFoo, mixedFoo, ""},
		"digit":                  {"foo2bar", "foo2bar", ""},
		"dash":                   {dashedFoo, dashedFoo, ""},
		"stops at a space":       {fooBar, foo, " bar"},
		"stops at a colon":       {fooColonBar, foo, ": bar"},
		"stops at an exclam":     {"foo!: bar", foo, "!: bar"},
		"stops at a parenthesis": {"foo(bar", foo, "(bar"},
		"stops at a trailing":    {"foo ", foo, " "},
		empty:                    {"", "", ""},
		"one space":              {" ", "", " "},
		"two spaces":             {twoSpaces, "", twoSpaces},
		"closing parenthesis":    {")", "", ")"},
		"leading space":          {" feat", "", " feat"},
		"leading and trailing":   {spacedFeat, "", spacedFeat},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, rest := takeWhile(tc.in, isTypeRune)
			if got != tc.want || rest != tc.rest {
				t.Errorf("type(%q) = (%q, %q), want (%q, %q)", tc.in, got, rest, tc.want, tc.rest)
			}
		})
	}
}

// Ported from parser.rs: the scope parser.
func TestScopeGrammar(t *testing.T) {
	t.Parallel()

	tests := map[string]struct{ in, want, rest string }{
		lowercase:    {foo, foo, ""},
		"upper":      {upperFoo, upperFoo, ""},
		"mixed-case": {mixedFoo, mixedFoo, ""},
		spaceName:    {fooBar, fooBar, ""},
		"dash":       {dashedFoo, dashedFoo, ""},
		"digits":     {"x86", "x86", ""},
		empty:        {"", "", ""},
		"closing":    {")", "", ")"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, rest := takeWhile(tc.in, isScopeRune)
			if got != tc.want || rest != tc.rest {
				t.Errorf("scope(%q) = (%q, %q), want (%q, %q)", tc.in, got, rest, tc.want, tc.rest)
			}
		})
	}
}

// Ported from parser.rs: the text parser takes a line, and fails on an empty one.
func TestDescriptionIsOneLine(t *testing.T) {
	t.Parallel()

	tests := map[string]struct{ in, want, rest string }{
		"word":      {foo, foo, ""},
		"words":     {fooBar, fooBar, ""},
		"newline":   {"foo bar\n", fooBar, "\n"},
		"lines":     {"foo\nbar\nbaz", foo, "\nbar\nbaz"},
		"carriage":  {"foo\r\nbar", foo, "\r\nbar"},
		empty:       {"", "", ""},
		"only line": {"\nfoo", "", "\nfoo"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, rest := takeWhile(tc.in, func(r rune) bool { return !isLineEnding(r) })
			if got != tc.want || rest != tc.rest {
				t.Errorf("text(%q) = (%q, %q), want (%q, %q)", tc.in, got, rest, tc.want, tc.rest)
			}
		})
	}
}

// Ported from parser.rs: the summary tests, through Parse (a summary alone is a whole message).
func TestSummaryGrammar(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		message                       string
		typ, scope, description       string
		hasScope, exclamation, accept bool
	}{
		"type and description":        {fooColonBar, foo, "", bar, false, false, true},
		"scope":                       {"foo(bar): baz", foo, bar, baz, true, false, true},
		"many spaces after the colon": {"foo(bar):     baz", foo, bar, baz, true, false, true},
		"dash in scope":               {"foo(bar-baz): qux", foo, "bar-baz", "qux", true, false, true},
		"exclamation mark":            {"foo!: bar", foo, "", bar, false, true, true},
		"scope and exclamation mark":  {"foo(bar)!: baz", foo, bar, baz, true, true, true},
		empty:                         {"", "", "", "", false, false, false},
		spaceName:                     {" ", "", "", "", false, false, false},
		"spaces":                      {twoSpaces, "", "", "", false, false, false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := Parse(tc.message)
			if (err == nil) != tc.accept {
				t.Fatalf("Parse(%q) error = %v, want success %v", tc.message, err, tc.accept)
			}
			if !tc.accept {
				return
			}
			if got.Type != tc.typ || got.Scope != tc.scope || got.Description != tc.description ||
				got.HasScope != tc.hasScope || got.Exclamation != tc.exclamation || got.Breaking != tc.exclamation {
				t.Errorf("Parse(%q) = %+v", tc.message, got)
			}
		})
	}
}

// Ported from parser.rs: the body parser stops before the footers.
func TestBodyGrammar(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		in, body, rest string
		found          bool
	}{
		lowercase:        {foo, foo, "", true},
		"capitalized":    {capFoo, capFoo, "", true},
		"all upper":      {upperFoo, upperFoo, "", true},
		"mixedCase":      {mixedFoo, mixedFoo, "", true},
		"code block":     {"    code block", "    code block", "", true},
		"emoji":          {"💃🏽", "💃🏽", "", true},
		"words":          {fooBar, fooBar, "", true},
		"paragraphs":     {"foo\nbar\n\nbaz", "foo\nbar\n\nbaz", "", true},
		"breaking space": {"foo\n\nBREAKING CHANGE: oops!", foo, "BREAKING CHANGE: oops!", true},
		"breaking dash":  {"foo\n\nBREAKING-CHANGE: bar", foo, "BREAKING-CHANGE: bar", true},
		"footer":         {"foo\n\nMy-Footer: bar", foo, "My-Footer: bar", true},
		"reference":      {"foo\n\nMy-Footer #bar", foo, "My-Footer #bar", true},
		empty:            {"", "", "", false},
		// oracle: the first line cannot be the body when it is a footer.
		"starts with a footer":    {fooColonBar, "", fooColonBar, false},
		"footer-like second line": {"foo\nbar: baz", "foo\nbar: baz", "", true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			body, rest, found := parseBody(tc.in)
			if body != tc.body || rest != tc.rest || found != tc.found {
				t.Errorf("body(%q) = (%q, %q, %v), want (%q, %q, %v)", tc.in, body, rest, found, tc.body, tc.rest, tc.found)
			}
		})
	}
}

// Ported from parser.rs: the footer parser.
func TestFooterGrammar(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		in    string
		want  Footer
		found bool
	}{
		"word":            {"hello: world", Footer{"hello", SeparatorValue, "world"}, true},
		"breaking phrase": {"BREAKING CHANGE: woops!", Footer{breakingPhrase, SeparatorValue, "woops!"}, true},
		"co-authored": {
			"Co-Authored-By: Marge Simpson <marge@simpsons.com>",
			Footer{"Co-Authored-By", SeparatorValue, "Marge Simpson <marge@simpsons.com>"}, true,
		},
		"reference":       {"Closes #12", Footer{"Closes", SeparatorRef, "12"}, true},
		"breaking arrow":  {"BREAKING-CHANGE: broken", Footer{breakingArrow, SeparatorValue, "broken"}, true},
		empty:             {"", Footer{}, false},
		spaceName:         {" ", Footer{}, false},
		"spaces":          {twoSpaces, Footer{}, false},
		"token only":      {foo, Footer{}, false},
		"no value":        {"foo:", Footer{}, false},
		"blank value":     {"foo: ", Footer{}, false},
		"trailing space":  {"foo ", Footer{}, false},
		"no reference":    {"foo #", Footer{}, false},
		"phrase only":     {breakingPhrase, Footer{}, false},
		"phrase no value": {"BREAKING CHANGE:", Footer{}, false},
		"dashed token":    {"Foo-Bar", Footer{}, false},
		"dashed no value": {"Foo-Bar: ", Footer{}, false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, rest, found, _ := parseFooter(tc.in)
			if found != tc.found || got != tc.want || (found && rest != "") {
				t.Errorf("footer(%q) = (%+v, %q, %v), want (%+v, %v)", tc.in, got, rest, found, tc.want, tc.found)
			}
		})
	}
}

func TestFooterIsBreaking(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		breakingPhrase:    true,
		breakingArrow:     true,
		"breaking-change": true,
		"Breaking-Change": true,
		"breaking change": true, // cannot be parsed as a token, but IsBreaking is a pure comparison
		"BREAKING":        false,
		"BREAKING_CHANGE": false,
		"Fixes":           false,
		"":                false,
	}
	for token, want := range tests {
		if got := (Footer{Token: token}).IsBreaking(); got != want {
			t.Errorf("Footer{%q}.IsBreaking() = %v, want %v", token, got, want)
		}
	}
}

func TestEqualFold(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		a, b string
		want bool
	}{
		"same":                        {typeFeat, typeFeat, true},
		"ascii case":                  {"FEAT", typeFeat, true},
		"mixed ascii case":            {"fEaT", "FeAt", true},
		"different":                   {typeFeat, typeFix, false},
		"prefix":                      {"fea", typeFeat, false},
		empty:                         {"", "", true},
		"empty and not":               {"", "a", false},
		"ligature fi is fi":           {"ﬁx", typeFix, true},
		"sharp s is ss":               {"ß", "SS", true},
		"capital sharp s is ss":       {"ẞ", "ss", true},
		"kelvin sign is k":            {"K", "k", true},
		"long s is s":                 {"ſ", "S", true},
		"dotted capital I is not i":   {"İ", "i", false},
		"dotless i is not i":          {"ı", "i", false},
		"greek sigma forms":           {"ς", "Σ", true},
		"not a prefix of the folding": {"ß", "s", false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := EqualFold(tc.a, tc.b); got != tc.want {
				t.Errorf("EqualFold(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
			if got := EqualFold(tc.b, tc.a); got != tc.want {
				t.Errorf("EqualFold(%q, %q) = %v, want %v", tc.b, tc.a, got, tc.want)
			}
		})
	}
}

// TestKnopeUsage covers what Knope does with a parsed commit: a type compared case-insensitively.
func TestKnopeUsage(t *testing.T) {
	t.Parallel()

	got, err := Parse("FIX(Scope)!: A bug\n\nbreaking-change: x\n\nFixes #1")
	if err != nil {
		t.Fatal(err)
	}
	if !EqualFold(got.Type, typeFix) || EqualFold(got.Type, typeFeat) {
		t.Errorf("type %q", got.Type)
	}
	if !got.Exclamation || !got.Breaking {
		t.Errorf("breaking flags: %+v", got)
	}
	if !got.Footers[0].IsBreaking() || got.Footers[1].IsBreaking() {
		t.Errorf("footers: %+v", got.Footers)
	}
}

func FuzzParse(f *testing.F) {
	seeds := []string{
		"", "feat: a", "feat(x)!: a\n\nbody\n\nBREAKING CHANGE: b\nCloses #1", "feat:\r\n\r\n", "a: b\n\nfoo:",
		"feat: a\n\n  \n", "\xff\xfe: x", "feat(\xff): a", "feat: a\n\nfoo: \xc3", strings.Repeat("a: b\n\n", 50),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, message string) {
		c, err := Parse(message)
		if err != nil {
			var parseErr *Error
			if !errors.As(err, &parseErr) || parseErr.Kind == 0 {
				t.Fatalf("Parse(%q) error %v is not an *Error with a kind", message, err)
			}
			return
		}
		if c.Type == "" || c.Description == "" || (c.HasScope && c.Scope == "") || (!c.HasBody && c.Body != "") {
			t.Fatalf("Parse(%q) = %+v violates the grammar", message, c)
		}
		if c.Exclamation && !c.Breaking {
			t.Fatalf("Parse(%q): exclamation without breaking", message)
		}
		for _, footer := range c.Footers {
			if footer.Token == "" || footer.Value == "" ||
				(footer.Separator != SeparatorValue && footer.Separator != SeparatorRef) {
				t.Fatalf("Parse(%q) has the footer %+v", message, footer)
			}
		}
	})
}
