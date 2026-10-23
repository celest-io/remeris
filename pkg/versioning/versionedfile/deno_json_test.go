package versionedfile_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/celest-io/remeris/pkg/versioning/semver"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	denoPath     = "deno.json"
	denoOld      = `"1.0.0"`
	denoNew      = "2.0.0"
	denoNewLit   = `"2.0.0"`
	denoBOM      = "\xef\xbb\xbf"
	denoParseErr = "Could not parse deno.json: "
	denoVerKey   = `"version": `
	denoTestdata = "testdata/deno_json"

	// denoBS is a backslash, built from its code so that no tool can decode an escape in the test tables.
	denoBS = string(rune(92))

	denoV100      = "1.0.0"
	denoV101      = "1.0.1"
	denoV110      = "1.1.0"
	denoV120      = "1.2.0"
	denoJSONExt   = ".json"
	denoNull      = "null"
	denoEmptyObj  = "{}"
	denoEmptyArr  = "[]"
	denoCloseNL   = "}\n"
	denoOneObject = `{"version": "1.0.0"}`
	denoNameObj   = `{"name": "x"}`
)

func denoNewFile(path, content string) (versionedfile.File, error) {
	return versionedfile.New(versionedfile.Config{Path: path, Format: versionedfile.DenoJson}, content, nil)
}

func mustDeno(t *testing.T, content string) versionedfile.File {
	t.Helper()
	f, err := denoNewFile(denoPath, content)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// denoSet applies SetVersion and returns the written content.
func denoSet(t *testing.T, in, version string) string {
	t.Helper()
	f, err := mustDeno(t, in).SetVersion(semver.MustParse(version), nil, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	acts := f.Write()
	if len(acts) != 1 {
		t.Fatalf("got %d actions", len(acts))
	}
	return acts[0].Content
}

// Ports of the unit tests of Knope's deno_json.rs.
func TestDenoRustPorts(t *testing.T) {
	t.Parallel()
	const (
		withVersion    = `{"name": "@scope/package", "version": "1.0.0"}`
		withoutVersion = `{"name": "@scope/package", "tasks": {"dev": "deno run main.ts"}}`
		withComment    = "// leading comment\n" + withVersion
	)
	tests := map[string]struct {
		in, path string
		set      string // version to set, or "" to only read
		want     string // version expected after, or "" for ErrNoVersion
	}{
		"rust with version":        {withVersion, denoPath, "", denoV100},
		"rust without version":     {withoutVersion, denoPath, "", ""},
		"rust set version":         {withVersion, denoPath, denoV110, denoV110},
		"rust set without version": {withoutVersion, denoPath, denoV100, denoV100},
		"rust comments":            {withComment, "deno.jsonc", "", denoV100},
		"rust set with comment":    {withComment, "deno.jsonc", denoV120, denoV120},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, err := denoNewFile(tc.path, tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if tc.set != "" {
				if f, err = f.SetVersion(semver.MustParse(tc.set), nil, versionedfile.Standard); err != nil {
					t.Fatal(err)
				}
			}
			got, err := f.Version()
			if tc.want == "" {
				if !errors.Is(err, versionedfile.ErrNoVersion) {
					t.Fatalf("got %v, %v; want ErrNoVersion", got, err)
				}
				return
			}
			if err != nil || got.String() != tc.want {
				t.Errorf("got %v, %v; want %s", got, err, tc.want)
			}
		})
	}
}

func TestDenoNoVersion(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"novers number":          `{"version": 1}`,
		"novers null":            `{"version": null}`,
		"novers object":          `{"version": {"a": "1.0.0"}}`,
		"novers invalid":         `{"version": "latest"}`,
		"novers empty string":    `{"version": ""}`,
		"novers nested only":     `{"a": {"version": "1.0.0"}}`,
		"novers in comment":      `{/* "version": "1.0.0" */ "a": 1}`,
		"novers in line comment": "{\n// \"version\": \"1.0.0\"\n\"a\": 1}",
		"novers in string":       `{"a": "\"version\": \"1.0.0\""}`,
		"novers array root":      `[{"version": "1.0.0"}]`,
		"novers scalar root":     `"x"`,
		"novers last duplicate":  `{"version": "1.0.0", "version": 3}`,
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := mustDeno(t, in).Version(); !errors.Is(err, versionedfile.ErrNoVersion) {
				t.Errorf("got %v, want ErrNoVersion", err)
			}
		})
	}
}

// TestDenoSplice checks the splice invariant: the output is the input with the bytes of the version value replaced.
// Each case gives the text before the value, the value as written and the text after it, so the span is known
// independently of the code under test.
func TestDenoSplice(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ before, old, after string }{
		"splice plain":             {`{"name": "x", ` + denoVerKey, denoOld, `}`},
		"splice final newline":     {`{"name": "x", ` + denoVerKey, denoOld, denoCloseNL},
		"splice no final newline":  {"{\n  " + denoVerKey, denoOld, "\n}"},
		"splice comment before":    {"// leading\n/* block */\n{\n  " + denoVerKey, denoOld, "\n}\n"},
		"splice comment inside":    {"{\n  // c\n  " + denoVerKey + "/* a */ ", denoOld, " /* b */, // d\n  \"x\": 1\n}\n"},
		"splice comment after":     {"{" + denoVerKey, denoOld, "} // trailing\n/* end */\n"},
		"splice trailing commas":   {"{\"a\": [1, 2,],\n  " + denoVerKey, denoOld, ",\n}\n"},
		"splice comma only":        {"{" + denoVerKey, denoOld, ",}"},
		"splice duplicate last":    {`{"version": "0.1.0", "a": 1, ` + denoVerKey, denoOld, `}`},
		"splice duplicate second":  {`{"version": "1.0.0", "version": `, `"0.1.0"`, `}`},
		"splice duplicate escaped": {`{"version": "0.1.0", "vers` + denoBS + `u0069on": `, denoOld, `}`},
		"splice nested before":     {`{"a": {"version": "9.9.9"}, ` + denoVerKey, denoOld, `}`},
		"splice nested after":      {"{" + denoVerKey, denoOld, `, "a": {"version": "9.9.9"}}`},
		"splice in comment":        {`{/* "version": "9.9.9" */ // "version": "8"` + "\n" + denoVerKey, denoOld, `}`},
		"splice in string":         {`{"d": "\"version\": \"9.9.9\"", ` + denoVerKey, denoOld, `}`},
		"splice CRLF":              {"{\r\n  \"name\": \"x\",\r\n  " + denoVerKey, denoOld, ",\r\n  \"y\": 1\r\n}\r\n"},
		"splice BOM":               {denoBOM + "{" + denoVerKey, denoOld, denoCloseNL},
		"splice BOM and comment":   {denoBOM + "// c\n{" + denoVerKey, denoOld, denoCloseNL},
		"splice tabs":              {"{\n\t\"a\":\t1,\n\t" + denoVerKey + "\t", denoOld, "\t\n}\n"},
		"splice unicode content":   {`{"é": "日本語 😀", ` + denoVerKey, denoOld, `, "ü": 1}`},
		"splice unicode escape":    {"{" + denoVerKey, `"1` + denoBS + `u002e0.0"`, `}`},
		"splice key escape":        {`{"vers` + denoBS + `u0069on": `, denoOld, `}`},
		"splice key all escaped":   {`{"` + denoBS + `u0076` + denoBS + `u0065rsion": `, denoOld, `}`},
		"splice escaped quote":     {"{" + denoVerKey, `"a` + denoBS + `"b"`, `}`},
		"splice escaped backslash": {"{" + denoVerKey, `"a` + denoBS + denoBS + `b"`, `}`},
		"splice escaped slash":     {"{" + denoVerKey, `"a` + denoBS + `/b` + denoBS + `n"`, `, "x": 1}`},
		"splice quote then comma":  {"{" + denoVerKey, `"a` + denoBS + `",` + denoBS + `"}"`, `}`},
		"splice build metadata":    {"{" + denoVerKey, `"1.0.0+build.5"`, `}`},
		"splice number":            {"{" + denoVerKey, `5`, `}`},
		"splice null":              {"{" + denoVerKey, denoNull, `}`},
		"splice array":             {"{" + denoVerKey, `["1.0.0", {"a": []}]`, `}`},
		"splice invalid string":    {"{" + denoVerKey, `"not a version"`, `}`},
		"splice empty string":      {"{" + denoVerKey, `""`, `}`},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := tc.before + tc.old + tc.after
			got := denoSet(t, in, denoNew)
			if want := tc.before + denoNewLit + tc.after; got != want {
				t.Errorf("got  %q\nwant %q", got, want)
			}
			f := mustDeno(t, got)
			if v, err := f.Version(); err != nil || v.String() != denoNew {
				t.Errorf("Version of output: %v, %v", v, err)
			}
			if again := denoSet(t, got, denoNew); again != got { // idempotence
				t.Errorf("not idempotent: %q", again)
			}
		})
	}
}

// TestDenoIdentity: setting the version the file already has changes no byte, also when it is written with escapes.
func TestDenoIdentity(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"identity plain":     "{\n  \"version\": \"1.0.0\"\n}\n",
		"identity escaped":   `{"version": "1` + denoBS + `u002e0.0"}`,
		"identity key":       `{"vers` + denoBS + `u0069on": "1` + denoBS + `u002e` + denoBS + `u0030.0"}`,
		"identity comments":  "// a\n{ /* b */ \"version\": \"1.0.0\", // c\n}\n",
		"identity CRLF":      "{\r\n\t\"version\": \"1.0.0\",\r\n}",
		"identity BOM":       denoBOM + "{\"version\": \"1.0.0\"}",
		"identity duplicate": `{"version": "0.1.0", "version": "1.0.0"}`,
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := denoSet(t, in, "1.0.0"); got != in {
				t.Errorf("got %q, want the input", got)
			}
		})
	}
}

func TestDenoInsert(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"insert inline":      {denoNameObj, `{"name": "x", "version": "2.0.0"}`},
		"insert compact":     {`{"a":1}`, `{"a":1, "version": "2.0.0"}`},
		"insert empty":       {denoEmptyObj, `{"version": "2.0.0"}`},
		"insert empty lines": {"{\n}", "{\"version\": \"2.0.0\"\n}"},
		"insert multiline":   {"{\n  \"name\": \"x\"\n}\n", "{\n  \"name\": \"x\",\n  \"version\": \"2.0.0\"\n}\n"},
		"insert trailing":    {"{\n  \"name\": \"x\",\n}\n", "{\n  \"name\": \"x\",\n  \"version\": \"2.0.0\",\n}\n"},
		"insert CRLF": {
			"{\r\n  \"name\": \"x\"\r\n}\r\n",
			"{\r\n  \"name\": \"x\",\r\n  \"version\": \"2.0.0\"\r\n}\r\n",
		},
		"insert tabs": {"{\n\t\"name\": \"x\"\n}", "{\n\t\"name\": \"x\",\n\t\"version\": \"2.0.0\"\n}"},
		"insert BOM":  {denoBOM + "{\"a\": 1}\n", denoBOM + "{\"a\": 1, \"version\": \"2.0.0\"}\n"},
		"insert line comment": {
			"{\n  \"name\": \"x\" // note\n}\n",
			"{\n  \"name\": \"x\", // note\n  \"version\": \"2.0.0\"\n}\n",
		},
		"insert comma comment": {
			"{\n  \"name\": \"x\", // note\n}\n",
			"{\n  \"name\": \"x\", // note\n  \"version\": \"2.0.0\",\n}\n",
		},
		"insert inline comment":       {"{\"a\": 1 // c\n}", "{\"a\": 1, // c\n\"version\": \"2.0.0\"\n}"},
		"insert inline comma comment": {"{\"a\": 1, // c\r\n}", "{\"a\": 1, // c\r\n\"version\": \"2.0.0\",\r\n}"},
		"insert lone CR in comment": {
			"{\"a\": 1 // x\ry\n}", "{\"a\": 1, // x\ry\n\"version\": \"2.0.0\"\n}",
		},
		"insert lone CR after comma": {
			"{\"a\": 1, // x\ry\n}", "{\"a\": 1, // x\ry\n\"version\": \"2.0.0\",\n}",
		},
		"insert CRLF after comment": {"{\"a\": 1 // x\r\n}", "{\"a\": 1, // x\r\n\"version\": \"2.0.0\"\r\n}"},
		"insert block comment":      {`{"a": 1 /* c */}`, `{"a": 1, "version": "2.0.0" /* c */}`},
		"insert nested only":        {`{"a": {"version": "1.0.0"}}`, `{"a": {"version": "1.0.0"}, "version": "2.0.0"}`},
		"insert after nested": {
			"{\n  \"a\": {\n    \"b\": 1\n  }\n}\n",
			"{\n  \"a\": {\n    \"b\": 1\n  },\n  \"version\": \"2.0.0\"\n}\n",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := denoSet(t, tc.in, denoNew)
			if got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
			if v, err := mustDeno(t, got).Version(); err != nil || v.String() != denoNew {
				t.Errorf("Version of output: %v, %v", v, err)
			}
		})
	}
}

func TestDenoParseErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"error empty":         "",
		"error comments only": "// nothing\n",
		"error unterminated":  `{"version": "1.0.0"`,
		"error garbage":       `not json`,
		"error single quote":  `{'version': '1.0.0'}`,
		"error unquoted key":  `{version: "1.0.0"}`,
		"error hex":           `{"a": 0x10}`,
		"error plus":          `{"a": +1}`,
		"error trailing":      `{"a": 1} {"b": 2}`,
		"error open comment":  `{"a": 1} /* x`,
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := denoNewFile(denoPath, in)
			if err == nil || !strings.HasPrefix(err.Error(), denoParseErr) {
				t.Errorf("got %v, want a %q error", err, denoParseErr)
			}
		})
	}
}

func TestDenoSetVersionOnNonObject(t *testing.T) {
	t.Parallel()
	for name, in := range map[string]string{"array root": `[1]`, "string root": `"x"`} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := mustDeno(t, in).SetVersion(semver.MustParse(denoNew), nil, versionedfile.Standard); err == nil {
				t.Error("want an error")
			}
		})
	}
}

func TestDenoDependencyIsNoOp(t *testing.T) {
	t.Parallel()
	dep := "@scope/other"
	f := mustDeno(t, denoOneObject)
	got, err := f.SetVersion(semver.MustParse(denoNew), &dep, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	if acts := got.Write(); acts != nil {
		t.Errorf("got actions %v, want none", acts)
	}
	if v, _ := got.Version(); v.String() != denoV100 {
		t.Errorf("version changed to %v", v)
	}
}

func TestDenoDiffAndPath(t *testing.T) {
	t.Parallel()
	f := mustDeno(t, denoOneObject)
	if f.Write() != nil {
		t.Error("a fresh file has pending writes")
	}
	f1, err := f.SetVersion(semver.MustParse("1.1.0"), nil, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	f2, err := f1.SetVersion(semver.MustParse("1.2.0"), nil, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	acts := f2.Write()
	if len(acts) != 1 || acts[0].Diff != "version = 1.1.0, version = 1.2.0" || acts[0].Path != denoPath ||
		acts[0].Content != `{"version": "1.2.0"}` {
		t.Errorf("unexpected actions %+v", acts)
	}
	if f.Path() != denoPath {
		t.Errorf("path %q", f.Path())
	}
	if v, _ := f.Version(); v.String() != denoV100 { // immutable
		t.Errorf("receiver changed: %v", v)
	}
}

func TestDenoHuge(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("// big\n{\n")
	for b.Len() < 8<<20 {
		b.WriteString("  \"key\": {\"version\": \"9.9.9\", \"list\": [1, 2, 3, \"x\"]}, // c\n")
	}
	b.WriteString("  " + denoVerKey + denoOld + "\n}\n")
	in := b.String()
	start := time.Now()
	got := denoSet(t, in, denoNew)
	if want := strings.TrimSuffix(in, denoOld+"\n}\n") + denoNewLit + "\n}\n"; got != want {
		t.Error("huge file edited wrongly")
	}
	if d := time.Since(start); d > 30*time.Second {
		t.Errorf("took %v", d)
	}
}

func TestDenoDeepNesting(t *testing.T) {
	t.Parallel()
	nested := func(depth int) string {
		return `{"a": ` + strings.Repeat("[", depth-1) + strings.Repeat("]", depth-1) + `, ` + denoVerKey + denoOld + "}"
	}
	in := nested(10000) // the root object counts as a level
	got := denoSet(t, in, denoNew)
	if want := strings.TrimSuffix(in, denoOld+"}") + denoNewLit + "}"; got != want {
		t.Error("deep file edited wrongly")
	}
	_, err := denoNewFile(denoPath, nested(10001))
	if err == nil || !strings.HasPrefix(err.Error(), denoParseErr) || !strings.Contains(err.Error(), "deeper than 10000") {
		t.Errorf("got %v, want a nesting error", err)
	}
	// A few megabytes of brackets must be an error, not a stack overflow.
	if _, err := denoNewFile(denoPath, strings.Repeat("[", 4<<20)); err == nil {
		t.Error("want an error")
	}
}

func TestDenoDepthIgnoresStringsAndComments(t *testing.T) {
	t.Parallel()
	in := `{"s": "` + strings.Repeat("[", 20000) + `", "t": "a` + denoBS + `"` + strings.Repeat("{", 20000) + `", ` +
		"// " + strings.Repeat("[", 20000) + "\n/* " + strings.Repeat("{", 20000) + " */ " + denoVerKey + denoOld + "}"
	if got := denoSet(t, in, denoNew); !strings.HasSuffix(got, denoNewLit+"}") {
		t.Error("brackets in strings and comments were counted")
	}
}

// TestDenoKnopeFixtures applies the version Knope's fixtures imply. The expected files are Knope's, plus the final
// newline that remeris keeps (Q6, see testdata/deviations.md). In test_package.jsonc Knope does not touch the file.
func TestDenoKnopeFixtures(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ name, ext, version string }{
		"golden first":        {"first", denoJSONExt, denoV110},
		"golden second":       {"second", denoJSONExt, "0.1.1"},
		"golden test package": {"test_package", denoJSONExt, denoV101},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := readDenoFixture(t, tc.name+".in"+tc.ext)
			want := readDenoFixture(t, tc.name+".out"+tc.ext)
			if got := denoSet(t, in, tc.version); got != want {
				t.Errorf("got\n%s\nwant\n%s", got, want)
			}
		})
	}
	t.Run("golden jsonc untouched", func(t *testing.T) {
		t.Parallel()
		in := readDenoFixture(t, "test_package.in.jsonc")
		if want := readDenoFixture(t, "test_package.out.jsonc"); in != want {
			t.Fatal("fixture changed")
		}
		f, err := denoNewFile("packages/test-package/deno.jsonc", in)
		if err != nil {
			t.Fatal(err)
		}
		if v, err := f.Version(); err != nil || v.String() != denoNew {
			t.Errorf("got %v, %v", v, err)
		}
		// The comment of the fixture survives an edit, which Knope would drop (Q6).
		got := denoSet(t, in, "2.0.1")
		if want := strings.Replace(
			in,
			`"2.0.0"`,
			`"2.0.1"`,
			1,
		); got != want ||
			!strings.Contains(got, "// This should be ignored") {
			t.Errorf("got\n%s", got)
		}
	})
}

func readDenoFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(denoTestdata, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// denoSkipSpace returns the offset of the first byte at or after i that is not white space or part of a comment. A line
// comment ends only at a line feed. This scanner and the ones below are the fuzz oracle: they share no code with
// the implementation and none with hujson.
func denoSkipSpace(s string, i int) int {
	for i < len(s) {
		switch {
		case s[i] == ' ' || s[i] == '\t' || s[i] == '\r' || s[i] == '\n':
			i++
		case strings.HasPrefix(s[i:], "//"):
			j := strings.IndexByte(s[i:], '\n')
			if j < 0 {
				return len(s)
			}
			i += j + 1
		case strings.HasPrefix(s[i:], "/*"):
			j := strings.Index(s[i+2:], "*/")
			if j < 0 {
				return len(s)
			}
			i += j + len("/**/")
		default:
			return i
		}
	}
	return i
}

// denoStringEnd returns the offset after the string that starts at s[i].
func denoStringEnd(s string, i int) int {
	for i++; i < len(s); i++ {
		switch s[i] {
		case denoBS[0]:
			i++
		case '"':
			return i + 1
		}
	}
	return len(s)
}

// denoValueEnd returns the offset after the value that starts at s[i].
func denoValueEnd(s string, i int) int {
	if i >= len(s) {
		return i
	}
	switch s[i] {
	case '"':
		return denoStringEnd(s, i)
	case '{', '[':
		for depth := 0; i < len(s); {
			switch s[i] {
			case '{', '[':
				depth++
				i++
			case '}', ']':
				depth--
				i++
				if depth == 0 {
					return i
				}
			case '"':
				i = denoStringEnd(s, i)
			case '/':
				if next := denoSkipSpace(s, i); next > i {
					i = next
				} else {
					i++
				}
			default:
				i++
			}
		}
		return i
	}
	for i < len(s) && !strings.ContainsRune(",}] \t\r\n/", rune(s[i])) {
		i++
	}
	return i
}

// denoLastVersion returns the span of the value of the last top level "version" member of the object in s.
func denoLastVersion(s string) (start, end int, found bool) {
	i := denoSkipSpace(s, 0)
	if i >= len(s) || s[i] != '{' {
		return 0, 0, false
	}
	i++
	for {
		i = denoSkipSpace(s, i)
		if i >= len(s) || s[i] != '"' {
			return start, end, found
		}
		keyEnd := denoStringEnd(s, i)
		var key string
		_ = json.Unmarshal([]byte(s[i:keyEnd]), &key)
		i = denoSkipSpace(s, keyEnd)
		if i >= len(s) || s[i] != ':' {
			return start, end, found
		}
		valueStart := denoSkipSpace(s, i+1)
		valueEnd := denoValueEnd(s, valueStart)
		if key == pubspecKey {
			start, end, found = valueStart, valueEnd, true
		}
		i = denoSkipSpace(s, valueEnd)
		if i < len(s) && s[i] == ',' {
			i++
		}
	}
}

// denoCheckOutput is the byte-level oracle of the fuzz test. A file with a version must come out as the input with
// the span of the last version value replaced by the new quoted version (and unchanged if it already had that
// version). A file without one must come out as the input plus the member and some white space and commas.
func denoCheckOutput(t *testing.T, in, out string) {
	t.Helper()
	if strings.HasPrefix(in, denoBOM) != strings.HasPrefix(out, denoBOM) {
		t.Fatalf("byte order mark changed:\n%q\n%q", in, out)
	}
	in, out = strings.TrimPrefix(in, denoBOM), strings.TrimPrefix(out, denoBOM)
	start, end, found := denoLastVersion(in)
	if found {
		var current string
		want := in[:start] + denoNewLit + in[end:]
		if json.Unmarshal([]byte(in[start:end]), &current) == nil && current == denoNew {
			want = in
		}
		if out != want {
			t.Fatalf("not a splice of [%d:%d]:\n%q\n%q", start, end, in, out)
		}
		return
	}
	vs, ve, ok := denoLastVersion(out)
	if !ok || out[vs:ve] != denoNewLit || vs < len(denoVerKey) || out[vs-len(denoVerKey):vs] != denoVerKey {
		t.Fatalf("no new version member in the output:\n%q\n%q", in, out)
	}
	rest := out[:vs-len(denoVerKey)] + out[ve:]
	j := 0
	for k := 0; k < len(rest); k++ {
		switch {
		case j < len(in) && in[j] == rest[k]:
			j++
		case !strings.ContainsRune(" ,\t\r\n", rune(rest[k])):
			t.Fatalf("the output changes more than the member:\n%q\n%q", in, out)
		}
	}
	if j != len(in) {
		t.Fatalf("the output lost bytes of the input:\n%q\n%q", in, out)
	}
}

func FuzzDenoSetVersion(f *testing.F) {
	seeds := []string{
		denoOneObject, denoEmptyObj, denoEmptyArr, ``, "\xef\xbb\xbf{\"version\": \"1.0.0\"}",
		"// c\n{\n  /* x */ \"version\": \"1.0.0\", // y\n}\n", `{"version": "1.0.0", "version": 3}`,
		`{"a": {"version": "1"}, "version": "1.0.0"}`, "{\r\n\t\"a\": 1,\r\n}", `{"a": 1 // c` + "\n}",
		"{\"a\": 1 // x\ry\n}", "{\"a\": 1, // x\ry\n}", "{\"a\": 1 // x\r\n}", "{\"a\": 1, // x\r\n}",
		`{"vers` + denoBS + `u0069on": "1` + denoBS + `u002e0.0"}`, `{"version": "a` + denoBS + `"b` + denoBS + denoBS + `"}`,
		`{"version": "1.0.0", "version": "0.1.0"}`, `{"a": "` + denoBS + `"version` + denoBS + `": 1"}`,
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		file, err := denoNewFile(denoPath, in)
		if err != nil {
			return
		}
		_, _ = file.Version()
		next, err := file.SetVersion(semver.MustParse(denoNew), nil, versionedfile.Standard)
		if err != nil {
			return
		}
		acts := next.Write()
		if len(acts) != 1 {
			t.Fatalf("got %d actions", len(acts))
		}
		out := acts[0].Content
		again, err := denoNewFile(denoPath, out)
		if err != nil {
			t.Fatalf("output does not parse: %v\n%q", err, out)
		}
		if v, err := again.Version(); err != nil || v.String() != denoNew {
			t.Fatalf("output version: %v, %v\n%q", v, err, out)
		}
		denoCheckOutput(t, in, out)
	})
}
