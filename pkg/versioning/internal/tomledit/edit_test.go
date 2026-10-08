package tomledit_test

import (
	"errors"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/internal/tomledit"
)

func k(names ...string) []tomledit.Key { return tomledit.Keys(names...) }

func TestSetString(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		in    string
		path  []tomledit.Key
		value string
		want  string
	}{
		"replace string keeps comment and spacing": {
			in:    "[package]\nname = \"x\"\nversion   =   \"1.0.0\"   # keep me\n\n[dependencies]\n",
			path:  k(keyPackage, keyVersion),
			value: newVersion,
			want:  "[package]\nname = \"x\"\nversion   =   \"2.0.0\"   # keep me\n\n[dependencies]\n",
		},
		"replace literal string": {
			in:    "version = '1.0.0'\n",
			path:  k(keyVersion),
			value: newVersion,
			want:  "version = \"2.0.0\"\n",
		},
		"replace multi-line string": {
			in:    "v = \"\"\"\n1\n2\"\"\"\nw = 1\n",
			path:  k("v"),
			value: "a",
			want:  "v = \"a\"\nw = 1\n",
		},
		"replace integer": {
			in:    "[x]\nversion = 3 # lock\n",
			path:  k("x", keyVersion),
			value: "4",
			want:  "[x]\nversion = \"4\" # lock\n",
		},
		"replace inline table": {
			in:    "d = { path = \"p\" }\ne = 1\n",
			path:  k("d"),
			value: "1.0",
			want:  "d = \"1.0\"\ne = 1\n",
		},
		"replace plain dependency": {
			in:    "[dependencies]\nd = \"1.0\"\nother = \"2.0\"\n",
			path:  k(keyDeps, "d"),
			value: newVersion,
			want:  "[dependencies]\nd = \"2.0.0\"\nother = \"2.0\"\n",
		},
		"replace inside inline table": {
			in:    "[dependencies]\nd = {path = \"../d\", version = \"0.1.0\" }\n",
			path:  k(keyDeps, "d", keyVersion),
			value: newVersion,
			want:  "[dependencies]\nd = {path = \"../d\", version = \"2.0.0\" }\n",
		},
		"replace through dotted keys": {
			in:    "[package]\nmeta.version = \"1\"\n",
			path:  k(keyPackage, "meta", keyVersion),
			value: "2",
			want:  "[package]\nmeta.version = \"2\"\n",
		},
		"replace in array of tables element": {
			in:    "[[package]]\nname = \"a\"\nversion = \"1\"\n[[package]]\nname = \"a\"\nversion = \"1\"\n",
			path:  []tomledit.Key{{Name: keyPackage, Index: 1}, {Name: keyVersion}},
			value: "2",
			want:  "[[package]]\nname = \"a\"\nversion = \"1\"\n[[package]]\nname = \"a\"\nversion = \"2\"\n",
		},
		"escapes": {
			in:    "v = \"x\"\n",
			path:  k("v"),
			value: "a\"b\\c\nd\te\x01\x7fé",
			want:  "v = \"a\\\"b\\\\c\\nd\\te\\u0001\\u007Fé\"\n",
		},
		"insert at end of table": {
			in:    "[package]\nname = \"x\"\n",
			path:  k(keyPackage, keyVersion),
			value: ver123,
			want:  "[package]\nname = \"x\"\nversion = \"1.2.3\"\n",
		},
		"insert before blank lines and comments of the next table": {
			in:    "[package]\nname = \"x\" # n\n\n# about deps\n[dependencies]\n",
			path:  k(keyPackage, keyVersion),
			value: ver123,
			want:  "[package]\nname = \"x\" # n\nversion = \"1.2.3\"\n\n# about deps\n[dependencies]\n",
		},
		"insert before trailing blank lines at the end of the file": {
			in:    "[package]\nname = \"x\"\n\n\n",
			path:  k(keyPackage, keyVersion),
			value: "1",
			want:  "[package]\nname = \"x\"\nversion = \"1\"\n\n\n",
		},
		"insert into header only table": {
			in:    "[a]\n# lonely\n[b]\nx = 1\n",
			path:  k("a", keyVersion),
			value: "1",
			want:  "[a]\nversion = \"1\"\n# lonely\n[b]\nx = 1\n",
		},
		"insert after multi-line array": {
			in:    "[a]\nl = [\n  1,\n] # c\n[b]\n",
			path:  k("a", keyVersion),
			value: "1",
			want:  "[a]\nl = [\n  1,\n] # c\nversion = \"1\"\n[b]\n",
		},
		"insert after dotted keys": {
			in:    "[a]\nb.c = 1\n[z]\n",
			path:  k("a", keyVersion),
			value: "1",
			want:  "[a]\nb.c = 1\nversion = \"1\"\n[z]\n",
		},
		"insert under dotted key parent": {
			in:    "[dependencies]\na.path = \"x\"\nz = 1\n",
			path:  k(keyDeps, "a", keyVersion),
			value: newVersion,
			want:  "[dependencies]\na.path = \"x\"\na.version = \"2.0.0\"\nz = 1\n",
		},
		"insert under dotted key parent after workspace flag": {
			in:    "[dependencies]\na.workspace = true\n",
			path:  k(keyDeps, "a", keyVersion),
			value: newVersion,
			want:  "[dependencies]\na.workspace = true\na.version = \"2.0.0\"\n",
		},
		"insert after the last line of the dotted parent": {
			in:    "[dependencies]\na.git = \"u\"\nz = 1\na.branch = \"b\"\n# c\ny = 2\n",
			path:  k(keyDeps, "a", keyVersion),
			value: newVersion,
			want:  "[dependencies]\na.git = \"u\"\nz = 1\na.branch = \"b\"\na.version = \"2.0.0\"\n# c\ny = 2\n",
		},
		"insert under quoted dotted parent": {
			in:    "[dependencies]\n\"a.b\".path = \"x\"\n",
			path:  k(keyDeps, "a.b", keyVersion),
			value: newVersion,
			want:  "[dependencies]\n\"a.b\".path = \"x\"\n\"a.b\".version = \"2.0.0\"\n",
		},
		"insert under literal quoted dotted parent": {
			in:    "[dependencies]\n'a'.path = 'x'\n",
			path:  k(keyDeps, "a", keyVersion),
			value: newVersion,
			want:  "[dependencies]\n'a'.path = 'x'\n'a'.version = \"2.0.0\"\n",
		},
		"insert under dotted parent at the root": {
			in:    "package.name = \"t\"\ndependencies.a.path = \"x\"\nz = 1\n",
			path:  k(keyDeps, "a", keyVersion),
			value: newVersion,
			want:  "package.name = \"t\"\ndependencies.a.path = \"x\"\ndependencies.a.version = \"2.0.0\"\nz = 1\n",
		},
		"insert under dotted parent in array of tables element": {
			in:    "[[t]]\na.path = \"x\"\n[[t]]\na.path = \"y\"\nz = 1\n",
			path:  []tomledit.Key{{Name: "t", Index: 1}, {Name: "a"}, {Name: keyVersion}},
			value: newVersion,
			want:  "[[t]]\na.path = \"x\"\n[[t]]\na.path = \"y\"\na.version = \"2.0.0\"\nz = 1\n",
		},
		"insert under nested dotted parent": {
			in:    "[dependencies]\na.b.path = \"x\"\nz = 1\n",
			path:  k(keyDeps, "a", keyVersion),
			value: newVersion,
			want:  "[dependencies]\na.b.path = \"x\"\na.version = \"2.0.0\"\nz = 1\n",
		},
		"insert under dotted parent keeps spaces around its dots": {
			in:    "[dependencies]\n  a . path   =   \"x\"  # c\n\nz=1\n",
			path:  k(keyDeps, "a", keyVersion),
			value: newVersion,
			want:  "[dependencies]\n  a . path   =   \"x\"  # c\na .version = \"2.0.0\"\n\nz=1\n",
		},
		"insert under dotted parent after a multi-line array": {
			in:    "[d]\na.path = \"x\"\na.f = [\n 1,\n]\nz = 1\n",
			path:  k("d", "a", keyVersion),
			value: newVersion,
			want:  "[d]\na.path = \"x\"\na.f = [\n 1,\n]\na.version = \"2.0.0\"\nz = 1\n",
		},
		"insert under dotted parent without final newline": {
			in:    "[d]\na.path = \"x\"",
			path:  k("d", "a", keyVersion),
			value: newVersion,
			want:  "[d]\na.path = \"x\"\na.version = \"2.0.0\"",
		},
		"insert under dotted parent in CRLF file": {
			in:    "[d]\r\na.path = \"x\"\r\nz = 1\r\n",
			path:  k("d", "a", keyVersion),
			value: newVersion,
			want:  "[d]\r\na.path = \"x\"\r\na.version = \"2.0.0\"\r\nz = 1\r\n",
		},
		"insert under dotted parent before the next table": {
			in:    "[d]\na.path = \"x\"\n\n[e]\ny = 1\n",
			path:  k("d", "a", keyVersion),
			value: newVersion,
			want:  "[d]\na.path = \"x\"\na.version = \"2.0.0\"\n\n[e]\ny = 1\n",
		},
		"insert without final newline": {
			in:    "[a]\nx = 1",
			path:  k("a", keyVersion),
			value: "1",
			want:  "[a]\nx = 1\nversion = \"1\"",
		},
		"insert into empty CRLF root": {
			in:    "\r\n[a]\r\n",
			path:  k(keyVersion),
			value: "1",
			want:  "version = \"1\"\r\n\r\n[a]\r\n",
		},
		"insert after last line without break in CRLF file": {
			in:    "[a]\r\nx = 1",
			path:  k("a", keyVersion),
			value: "1",
			want:  "[a]\r\nx = 1\r\nversion = \"1\"",
		},
		"insert into LF file with later CRLF": {
			in:    "[a]\nx = 1\r\n",
			path:  k("a", keyVersion),
			value: "1",
			want:  "[a]\nx = 1\r\nversion = \"1\"\n",
		},
		"insert into file without any line break": {
			in:    "",
			path:  k(keyVersion),
			value: "1",
			want:  "version = \"1\"\n",
		},
		"insert keeps CRLF": {
			in:    "[a]\r\nx = 1\r\n[b]\r\n",
			path:  k("a", keyVersion),
			value: "1",
			want:  "[a]\r\nx = 1\r\nversion = \"1\"\r\n[b]\r\n",
		},
		"insert in dependency table": {
			in:    "[dependencies.d]\npath = \"../d\"\n\n[dev-dependencies]\n",
			path:  k(keyDeps, "d", keyVersion),
			value: newVersion,
			want:  "[dependencies.d]\npath = \"../d\"\nversion = \"2.0.0\"\n\n[dev-dependencies]\n",
		},
		"insert at root of empty document": {
			in:    "",
			path:  k(keyVersion),
			value: ver123,
			want:  "version = \"1.2.3\"\n",
		},
		"insert at root before first header and its comments": {
			in:    "name = \"app\"\n\n# deps\n[dependencies]\n",
			path:  k(keyVersion),
			value: ver123,
			want:  "name = \"app\"\nversion = \"1.2.3\"\n\n# deps\n[dependencies]\n",
		},
		"insert at root with only a header": {
			in:    "# gleam\n[dependencies]\nx = \"1\"\n",
			path:  k(keyVersion),
			value: ver123,
			want:  "version = \"1.2.3\"\n# gleam\n[dependencies]\nx = \"1\"\n",
		},
		"insert into non-empty inline table": {
			in:    "[dependencies]\nd = {path = \"../d\"}\ne = 1\n",
			path:  k(keyDeps, "d", keyVersion),
			value: newVersion,
			want:  "[dependencies]\nd = {path = \"../d\", version = \"2.0.0\" }\ne = 1\n",
		},
		"insert into spaced inline table": {
			in:    "d = { path = \"../d\" } # c\n",
			path:  k("d", keyVersion),
			value: "1",
			want:  "d = { path = \"../d\" , version = \"1\" } # c\n",
		},
		"insert into inline table with trailing comma": {
			in:    "d = { path = \"../d\", }\n",
			path:  k("d", keyVersion),
			value: "1",
			want:  "d = { path = \"../d\", version = \"1\" , }\n",
		},
		"insert into empty inline table": {
			in:    "d = {}\n",
			path:  k("d", keyVersion),
			value: "1",
			want:  "d = { version = \"1\" }\n",
		},
		"insert into empty inline table with space": {
			in:    "d = { }\n",
			path:  k("d", keyVersion),
			value: "1",
			want:  "d = { version = \"1\"  }\n",
		},
		"insert into nested inline table": {
			in:    "d = { n = { a = 1 } }\n",
			path:  k("d", "n", keyVersion),
			value: "1",
			want:  "d = { n = { a = 1 , version = \"1\" } }\n",
		},
		"insert quoted key": {
			in:    "[a]\n",
			path:  k("a", "my key"),
			value: "1",
			want:  "[a]\n\"my key\" = \"1\"\n",
		},
		"insert into array of tables element": {
			in: "[[package]]\nname = \"a\"\n\n[[package]]\nname = \"b\"\n\n[[package]]\nname = \"c\"\n",
			path: []tomledit.Key{
				{Name: keyPackage, Index: 1}, {Name: keyVersion},
			},
			value: "9",
			want:  "[[package]]\nname = \"a\"\n\n[[package]]\nname = \"b\"\nversion = \"9\"\n\n[[package]]\nname = \"c\"\n",
		},
		"insert into last array of tables element": {
			in:    "[[package]]\nname = \"a\"\n",
			path:  []tomledit.Key{{Name: keyPackage, Index: 0}, {Name: keyVersion}},
			value: "9",
			want:  "[[package]]\nname = \"a\"\nversion = \"9\"\n",
		},
		"insert into header only array of tables element": {
			in:    "[[package]]\n[[package]]\nname = \"b\"\n",
			path:  []tomledit.Key{{Name: keyPackage, Index: 0}, {Name: keyName}},
			value: "a",
			want:  "[[package]]\nname = \"a\"\n[[package]]\nname = \"b\"\n",
		},
		"insert below nested table of the element": {
			in:    "[[package]]\nname = \"a\"\n[package.sub]\nx = 1\n",
			path:  []tomledit.Key{{Name: keyPackage}, {Name: keyVersion}},
			value: "9",
			want:  "[[package]]\nname = \"a\"\nversion = \"9\"\n[package.sub]\nx = 1\n",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := mustParse(t, tt.in)
			if err := doc.SetString(tt.path, tt.value); err != nil {
				t.Fatalf("SetString: %v", err)
			}
			if got := string(doc.Bytes()); got != tt.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, tt.want)
			}
			// The edited document must be usable again.
			if _, err := tomledit.Parse(doc.Bytes()); err != nil {
				t.Errorf("result does not parse: %v", err)
			}
			if got, ok := doc.LookupIn(tt.path); !ok || got.Kind != tomledit.String || got.Str != tt.value {
				t.Errorf("LookupIn after edit = %+v, %v; want string %q", got, ok, tt.value)
			}
		})
	}
}

func TestSetStringErrors(t *testing.T) {
	t.Parallel()
	const src = "[a]\nx = 1\n[b.c]\ny = 1\n[d]\ne.f = 1\ng = [1]\ni = { j.k = 1 }\n[[h]]\n"
	tests := map[string]struct {
		path []tomledit.Key
		want error
	}{
		"empty path":               {path: nil, want: tomledit.ErrNoParent},
		"missing parent":           {path: k("zzz", keyVersion), want: tomledit.ErrNoParent},
		"implicit parent":          {path: k("b", keyVersion), want: tomledit.ErrNotInsertable},
		"dotted key inside inline": {path: k("d", "i", "j", keyVersion), want: tomledit.ErrNotInsertable},
		"scalar parent":            {path: k("a", "x", keyVersion), want: tomledit.ErrNotInsertable},
		"array parent":             {path: k("d", "g", keyVersion), want: tomledit.ErrNotInsertable},
		"existing table":           {path: k("b", "c"), want: tomledit.ErrNotValue},
		"existing array of tables": {path: k("h"), want: tomledit.ErrNotValue},
		"array of tables bad index": {
			path: []tomledit.Key{{Name: "h", Index: 3}, {Name: keyVersion}},
			want: tomledit.ErrNoParent,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := mustParse(t, src)
			err := doc.SetString(tt.path, "v")
			if !errors.Is(err, tt.want) {
				t.Errorf("SetString error = %v, want %v", err, tt.want)
			}
			if got := string(doc.Bytes()); got != src {
				t.Errorf("document changed on error: %q", got)
			}
		})
	}
	if err := mustParse(t, src).SetString(k("a", "\xff"), "v"); !errors.Is(err, tomledit.ErrUnrepresentable) {
		t.Errorf("invalid UTF-8 key error = %v, want ErrUnrepresentable", err)
	}
	if err := mustParse(t, src).SetString(k("a", "y"), "\xff"); !errors.Is(err, tomledit.ErrUnrepresentable) {
		t.Errorf("invalid UTF-8 error = %v, want ErrUnrepresentable", err)
	}
}

func TestReplaceStringContents(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		in    string
		path  []tomledit.Key
		value string
		want  string
	}{
		"basic": {
			in:    "[project]\nname = \"x\"\nversion = \"0.1.0\"  # c\n",
			path:  k("project", keyVersion),
			value: newVersion,
			want:  "[project]\nname = \"x\"\nversion = \"2.0.0\"  # c\n",
		},
		"literal keeps single quotes": {
			in:    "[tool.poetry]\nversion = '0.1.0'\n",
			path:  k("tool", "poetry", keyVersion),
			value: newVersion,
			want:  "[tool.poetry]\nversion = '2.0.0'\n",
		},
		"multi-line basic keeps delimiters": {
			in:    "v = \"\"\"\n0.1.0\"\"\"\n",
			path:  k("v"),
			value: "2",
			want:  "v = \"\"\"2\"\"\"\n",
		},
		"empty string": {
			in:    "v = \"\"\n",
			path:  k("v"),
			value: "1",
			want:  "v = \"1\"\n",
		},
		"to empty": {
			in:    "v = 'abc'\n",
			path:  k("v"),
			value: "",
			want:  "v = ''\n",
		},
		"escapes in basic": {
			in:    "v = \"a\"\n",
			path:  k("v"),
			value: "q\"\\\n",
			want:  "v = \"q\\\"\\\\\\n\"\n",
		},
		"newline allowed in multi-line": {
			in:    "v = '''a'''\n",
			path:  k("v"),
			value: "x\ny",
			want:  "v = '''x\ny'''\n",
		},
		"leading newline kept in multi-line basic": {
			in:    "a = \"\"\"\nhi\"\"\"\n",
			path:  k("a"),
			value: "\nx\ny",
			want:  "a = \"\"\"\n\nx\ny\"\"\"\n",
		},
		"leading CRLF kept in multi-line basic": {
			in:    "a = \"\"\"hi\"\"\"\n",
			path:  k("a"),
			value: "\r\nx",
			want:  "a = \"\"\"\n\\r\nx\"\"\"\n",
		},
		"leading newline kept in multi-line literal": {
			in:    "a = '''\nhi'''\n",
			path:  k("a"),
			value: "\nx\ny",
			want:  "a = '''\n\nx\ny'''\n",
		},
		"CRLF inside multi-line literal": {
			in:    "a = '''hi'''\n",
			path:  k("a"),
			value: "x\r\ny",
			want:  "a = '''x\r\ny'''\n",
		},
		"escapes already in the old value": {
			in:    "v = \"a\\nb\" # t\n",
			path:  k("v"),
			value: "z",
			want:  "v = \"z\" # t\n",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := mustParse(t, tt.in)
			if err := doc.ReplaceStringContents(tt.path, tt.value); err != nil {
				t.Fatalf("ReplaceStringContents: %v", err)
			}
			if got := string(doc.Bytes()); got != tt.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, tt.want)
			}
			if got, ok := doc.LookupIn(tt.path); !ok || got.Str != tt.value {
				t.Errorf("LookupIn after edit = %+v, %v; want %q", got, ok, tt.value)
			}
		})
	}

	errTests := map[string]struct {
		path  []tomledit.Key
		value string
		want  error
	}{
		"not a string":              {path: k("n"), value: "1", want: tomledit.ErrNotString},
		"missing":                   {path: k("zzz"), value: "1", want: tomledit.ErrNoParent},
		"quote in literal":          {path: k("lit"), value: "it's", want: tomledit.ErrUnrepresentable},
		"newline in literal":        {path: k("lit"), value: "a\nb", want: tomledit.ErrUnrepresentable},
		"control in literal":        {path: k("lit"), value: "a\x01", want: tomledit.ErrUnrepresentable},
		"bad utf-8":                 {path: k("s"), value: "\xff", want: tomledit.ErrUnrepresentable},
		"bare CR in multi-line":     {path: k("ml"), value: "a\rb", want: tomledit.ErrUnrepresentable},
		"trailing CR in multi-line": {path: k("ml"), value: "a\r", want: tomledit.ErrUnrepresentable},
		"table is not a value":      {path: k("t"), value: "1", want: tomledit.ErrNotString},
	}
	const src = "n = 1\nlit = 'x'\ns = \"x\"\nml = '''x'''\n[t]\n"
	for name, tt := range errTests {
		t.Run("error/"+name, func(t *testing.T) {
			t.Parallel()
			doc := mustParse(t, src)
			if err := doc.ReplaceStringContents(tt.path, tt.value); !errors.Is(err, tt.want) {
				t.Errorf("error = %v, want %v", err, tt.want)
			}
			if got := string(doc.Bytes()); got != src {
				t.Errorf("document changed on error: %q", got)
			}
		})
	}
}

// TestKnopeShapes mirrors the edits Knope makes to Cargo.toml, Cargo.lock, gleam.toml and pyproject.toml
// (crates/knope-versioning/src/versioned_file) on inputs taken from its tests and fixtures.
func TestKnopeShapes(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		in   string
		edit func(*tomledit.Document) error
		want string
	}{
		"cargo dependencies fixture shapes": {
			in: `[package]
name = "tester"
version = "1.2.3-rc.0"

[dependencies]
knope-versioning = "0.1.0"
other = {path = "../other"}
complex-requirement-in-object = { version = "1.2.*" }

[dev-dependencies]
knope-versioning = {path = "../blah", version = "0.1.0" }

[workspace.dependencies]
knope-versioning = "0.1.0"
`,
			edit: func(d *tomledit.Document) error {
				for _, p := range [][]string{
					{keyDeps, "knope-versioning"},
					{keyWorkspace, keyDeps, "knope-versioning"},
				} {
					if err := d.SetString(tomledit.Keys(p...), "0.2.0"); err != nil {
						return err
					}
				}
				if err := d.SetString(k("dev-dependencies", "knope-versioning", keyVersion), "0.2.0"); err != nil {
					return err
				}
				if err := d.SetString(k(keyDeps, "other", keyVersion), "0.2.0"); err != nil {
					return err
				}
				return d.SetString(k(keyDeps, "complex-requirement-in-object", keyVersion), newVersion)
			},
			want: `[package]
name = "tester"
version = "1.2.3-rc.0"

[dependencies]
knope-versioning = "0.2.0"
other = {path = "../other", version = "0.2.0" }
complex-requirement-in-object = { version = "2.0.0" }

[dev-dependencies]
knope-versioning = {path = "../blah", version = "0.2.0" }

[workspace.dependencies]
knope-versioning = "0.2.0"
`,
		},
		"cargo workspace package version": {
			in: "[workspace]\nmembers = [\"first\"]\n\n[workspace.package]\nversion = \"1.0.0\"\n\n" +
				"[workspace.dependencies]\nfirst-package = { path = \"first\", version = \"1.0.0\" }\n",
			edit: func(d *tomledit.Document) error {
				return d.SetString(k(keyWorkspace, keyPackage, keyVersion), "1.1.0")
			},
			want: "[workspace]\nmembers = [\"first\"]\n\n[workspace.package]\nversion = \"1.1.0\"\n\n" +
				"[workspace.dependencies]\nfirst-package = { path = \"first\", version = \"1.0.0\" }\n",
		},
		"cargo lock package without version": {
			in: "# This file is automatically @generated by Cargo.\nversion = 3\n\n[[package]]\nname = \"first-package\"\n" +
				"version = \"1.0.0\"\n\n[[package]]\nname = \"second-package\"\n\n[[package]]\nname = \"serde\"\nversion = \"1.0.0\"\n" +
				"source = \"registry+https://github.com/rust-lang/crates.io-index\"\n",
			edit: func(d *tomledit.Document) error {
				if err := d.SetString([]tomledit.Key{{Name: keyPackage, Index: 0}, {Name: keyVersion}}, "1.1.0"); err != nil {
					return err
				}
				return d.SetString([]tomledit.Key{{Name: keyPackage, Index: 1}, {Name: keyVersion}}, "0.2.0")
			},
			want: "# This file is automatically @generated by Cargo.\nversion = 3\n\n[[package]]\nname = \"first-package\"\n" +
				"version = \"1.1.0\"\n\n[[package]]\nname = \"second-package\"\nversion = \"0.2.0\"\n\n[[package]]\nname = \"serde\"\n" +
				"version = \"1.0.0\"\nsource = \"registry+https://github.com/rust-lang/crates.io-index\"\n",
		},
		"gleam replace": {
			in:   "name = \"app\"\nversion = \"1.2.3\"\n\n[dependencies]\ngleam_stdlib = \">= 0.34.0 and < 2.0.0\"\n",
			edit: func(d *tomledit.Document) error { return d.SetString(k(keyVersion), newVersion) },
			want: "name = \"app\"\nversion = \"2.0.0\"\n\n[dependencies]\ngleam_stdlib = \">= 0.34.0 and < 2.0.0\"\n",
		},
		"gleam insert": {
			in:   "name = \"app\"\n\n[dependencies]\ngleam_stdlib = \">= 0.34.0 and < 2.0.0\"\n",
			edit: func(d *tomledit.Document) error { return d.SetString(k(keyVersion), newVersion) },
			want: "name = \"app\"\nversion = \"2.0.0\"\n\n[dependencies]\ngleam_stdlib = \">= 0.34.0 and < 2.0.0\"\n",
		},
		"pyproject both versions": {
			in: "[tool.poetry]\nname = \"tester\"\nversion = \"0.1.0-rc.0\"\n\n[project]\nname = 'tester'\nversion = '0.1.0-rc.0' # same\n",
			edit: func(d *tomledit.Document) error {
				if err := d.ReplaceStringContents(k("tool", "poetry", keyVersion), "0.2.0"); err != nil {
					return err
				}
				return d.ReplaceStringContents(k("project", keyVersion), "0.2.0")
			},
			want: "[tool.poetry]\nname = \"tester\"\nversion = \"0.2.0\"\n\n[project]\nname = 'tester'\nversion = '0.2.0' # same\n",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := mustParse(t, tt.in)
			if err := tt.edit(doc); err != nil {
				t.Fatalf("edit: %v", err)
			}
			if got := string(doc.Bytes()); got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}
