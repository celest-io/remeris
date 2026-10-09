package tomledit_test

import (
	"errors"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/celest-io/remeris/pkg/versioning/internal/tomledit"
)

const (
	keyVersion   = "version"
	keyPackage   = "package"
	keyName      = "name"
	newVersion   = "2.0.0"
	keyDeps      = "dependencies"
	keyWorkspace = "workspace"
	keyBin       = "bin"
	ver123       = "1.2.3"
)

func mustParse(t *testing.T, src string) *tomledit.Document {
	t.Helper()
	doc, err := tomledit.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return doc
}

func TestLookup(t *testing.T) {
	t.Parallel()
	const src = `# top
[package]
name = "demo" # c
version.workspace = true
edition = 2_021
resolver = 'it'
ok = true
ratio = 1.5
when = 1979-05-27
list = [1,
  2]

[workspace.package]
version = "1.0.0"

[dependencies]
a = "1.0"
b = { path = "../b", nested = { deep = "x" } }
"dotted.key" = "q"
c.d.e = "f"

[dependencies.t]
version = "3.0"

[[bin]]
name = "first"

[[bin]]
name = "second"

[[bin.extra]]
n = 1

[a . "b c"]
x = 1
`
	doc := mustParse(t, src)
	tests := map[string]struct {
		path []string
		kind tomledit.Kind
		str  string
		num  int64
		span string
	}{
		"header table value": {path: []string{keyPackage, keyName}, kind: tomledit.String, str: "demo", span: `"demo"`},
		"nested header": {
			path: []string{keyWorkspace, keyPackage, keyVersion},
			kind: tomledit.String,
			str:  "1.0.0",
			span: `"1.0.0"`,
		},
		"dotted key is table": {path: []string{keyPackage, keyVersion}, kind: tomledit.Table},
		"dotted key leaf":     {path: []string{keyPackage, keyVersion, keyWorkspace}, kind: tomledit.Bool, span: "true"},
		"integer underscore":  {path: []string{keyPackage, "edition"}, kind: tomledit.Integer, num: 2021, span: "2_021"},
		"literal string":      {path: []string{keyPackage, "resolver"}, kind: tomledit.String, str: "it", span: "'it'"},
		"float":               {path: []string{keyPackage, "ratio"}, kind: tomledit.Float, span: "1.5"},
		"datetime":            {path: []string{keyPackage, "when"}, kind: tomledit.Datetime, span: "1979-05-27"},
		"multi-line array":    {path: []string{keyPackage, "list"}, kind: tomledit.Array, span: "[1,\n  2]"},
		"plain dependency":    {path: []string{keyDeps, "a"}, kind: tomledit.String, str: "1.0", span: `"1.0"`},
		"inline table": {
			path: []string{keyDeps, "b"}, kind: tomledit.InlineTable,
			span: `{ path = "../b", nested = { deep = "x" } }`,
		},
		"inside inline table": {
			path: []string{keyDeps, "b", "path"},
			kind: tomledit.String,
			str:  "../b",
			span: `"../b"`,
		},
		"nested inline table": {
			path: []string{keyDeps, "b", "nested", "deep"},
			kind: tomledit.String,
			str:  "x",
			span: `"x"`,
		},
		"quoted key with dot": {path: []string{keyDeps, "dotted.key"}, kind: tomledit.String, str: "q", span: `"q"`},
		"dotted key chain":    {path: []string{keyDeps, "c", "d", "e"}, kind: tomledit.String, str: "f", span: `"f"`},
		"table dependency":    {path: []string{keyDeps, "t"}, kind: tomledit.Table},
		"table dep value": {
			path: []string{keyDeps, "t", keyVersion},
			kind: tomledit.String,
			str:  "3.0",
			span: `"3.0"`,
		},
		"array of tables":     {path: []string{keyBin}, kind: tomledit.ArrayOfTables},
		"last element":        {path: []string{keyBin, keyName}, kind: tomledit.String, str: "second", span: `"second"`},
		"sub array of tables": {path: []string{keyBin, "extra", "n"}, kind: tomledit.Integer, num: 1, span: "1"},
		"quoted header key":   {path: []string{"a", "b c", "x"}, kind: tomledit.Integer, num: 1, span: "1"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := doc.Lookup(tt.path...)
			if !ok {
				t.Fatalf("Lookup(%v) not found", tt.path)
			}
			if got.Kind != tt.kind || got.Str != tt.str && tt.kind == tomledit.String || got.Int != tt.num {
				t.Errorf("Lookup(%v) = %+v, want kind %v str %q int %d", tt.path, got, tt.kind, tt.str, tt.num)
			}
			if tt.span != "" && src[got.Span[0]:got.Span[1]] != tt.span {
				t.Errorf("span text = %q, want %q", src[got.Span[0]:got.Span[1]], tt.span)
			}
		})
	}

	for name, path := range map[string][]string{
		"missing key":       {keyPackage, "nope"},
		"through a scalar":  {keyPackage, keyName, "x"},
		"missing table":     {"nope", keyName},
		"through an array":  {keyPackage, "list", "x"},
		"unsplit dotted":    {keyDeps, "c.d"},
		"dotted key quoted": {keyDeps, "dotted", "key"},
	} {
		t.Run("not found/"+name, func(t *testing.T) {
			t.Parallel()
			if got, ok := doc.Lookup(path...); ok {
				t.Errorf("Lookup(%v) = %+v, want not found", path, got)
			}
		})
	}

	if got := doc.ArrayOfTablesLen(keyBin); got != 2 {
		t.Errorf("ArrayOfTablesLen(bin) = %d, want 2", got)
	}
	if got := doc.ArrayOfTablesLen(keyPackage); got != 0 {
		t.Errorf("ArrayOfTablesLen(package) = %d, want 0", got)
	}
}

func TestLookupIn(t *testing.T) {
	t.Parallel()
	doc := mustParse(t, "[[package]]\nname = \"a\"\n\n[[package]]\nname = \"b\"\n[package.sub]\nx = \"y\"\n")
	tests := map[string]struct {
		path []tomledit.Key
		want string
		ok   bool
	}{
		"first":        {path: []tomledit.Key{{Name: keyPackage, Index: 0}, {Name: keyName}}, want: "a", ok: true},
		"second":       {path: []tomledit.Key{{Name: keyPackage, Index: 1}, {Name: keyName}}, want: "b", ok: true},
		"sub table":    {path: []tomledit.Key{{Name: keyPackage, Index: 1}, {Name: "sub"}, {Name: "x"}}, want: "y", ok: true},
		"first no sub": {path: []tomledit.Key{{Name: keyPackage, Index: 0}, {Name: "sub"}, {Name: "x"}}},
		"out of range": {path: []tomledit.Key{{Name: keyPackage, Index: 2}, {Name: keyName}}},
		"negative":     {path: []tomledit.Key{{Name: keyPackage, Index: -1}, {Name: keyName}}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := doc.LookupIn(tt.path)
			if ok != tt.ok || got.Str != tt.want {
				t.Errorf("LookupIn = %+v, %v; want %q, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"unterminated string": "a = \"x\n",
		"missing value":       "a =\n",
		"bad header":          "[a\nb = 1\n",
		"duplicate key":       "a = 1\na = 2\n",
		"duplicate table":     "[a]\n[a]\n",
		"scalar then table":   "a = 1\n[a.b]\n",
		"inline then header":  "a = {}\n[a.b]\n",
		"aot then table":      "[[a]]\n[a]\n",
		"dotted then header":  "[x]\na.b = 1\n[x.a]\n",
		"duplicate inline":    "a = { b = 1, b = 2 }\n",
		"invalid scalar":      "0=0000-",
		"invalid date":        "a = 1979-13-45\n",
	}
	for name, src := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := tomledit.Parse([]byte(src))
			if !errors.Is(err, tomledit.ErrSyntax) {
				t.Errorf("Parse error = %v, want ErrSyntax", err)
			}
		})
	}
}

func TestParseKeepsBytes(t *testing.T) {
	t.Parallel()
	const src = "# c\r\n[a]\r\nb = 1\r\n"
	if got := string(mustParse(t, src).Bytes()); got != src {
		t.Errorf("Bytes() = %q, want %q", got, src)
	}
}

const bomMark = "\xEF\xBB\xBF"

func TestParseByteOrderMark(t *testing.T) {
	t.Parallel()
	const body = "[package]\nversion = \"1.0.0\"\n"
	doc := mustParse(t, bomMark+body)
	if got := string(doc.Bytes()); got != bomMark+body {
		t.Errorf("Bytes() = %q, want the document with its byte order mark", got)
	}
	v, ok := doc.Lookup(keyPackage, keyVersion)
	if !ok || v.Str != "1.0.0" {
		t.Fatalf("Lookup = %+v, %v", v, ok)
	}
	// Spans are offsets in the bytes given to Parse, so they count the mark.
	if got := (bomMark + body)[v.Span[0]:v.Span[1]]; got != `"1.0.0"` {
		t.Errorf("span %v selects %q", v.Span, got)
	}
	mustParse(t, bomMark)
}

func TestParseByteOrderMarkErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]string{"bad key": "a = 1\n= 2\n", "bad value": "a = 1\nb = =\n"}
	for name, src := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var plain, marked *toml.DecodeError
			_, errPlain := tomledit.Parse([]byte(src))
			_, errMarked := tomledit.Parse([]byte(bomMark + src))
			if !errors.As(errPlain, &plain) || !errors.As(errMarked, &marked) {
				t.Fatalf("errors %v, %v do not wrap a DecodeError", errPlain, errMarked)
			}
			pr, _ := plain.Position()
			mr, _ := marked.Position()
			if pr != mr || plain.Error() != marked.Error() {
				t.Errorf("row %d (%q) with a byte order mark, want %d (%q)", mr, marked, pr, plain)
			}
		})
	}
}
