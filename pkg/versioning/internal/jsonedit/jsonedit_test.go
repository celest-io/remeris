package jsonedit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/internal/jsonedit"
)

const (
	numOne     = "1.0"
	numE16     = "1e16"
	numNegZero = "-0.0"
)

func roundTrip(t *testing.T, src string) string {
	t.Helper()
	v, err := jsonedit.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	return string(jsonedit.Marshal(v))
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ in, want string }{
		"scalars":         {`null`, `null`},
		"true":            {` true `, `true`},
		"empty object":    {`{ }`, `{}`},
		"empty array":     {`[ ]`, `[]`},
		"nested empty":    {`{"a":{},"b":[],"c":[{}]}`, "{\n  \"a\": {},\n  \"b\": [],\n  \"c\": [\n    {}\n  ]\n}"},
		"compact object":  {`{"b":1,"a":[1,2]}`, "{\n  \"b\": 1,\n  \"a\": [\n    1,\n    2\n  ]\n}"},
		"unicode":         {`{"k":"héllo ☃ 😀"}`, "{\n  \"k\": \"héllo ☃ 😀\"\n}"},
		"unicode escape":  {`"é😀"`, `"é😀"`},
		"escapes":         {`"a\"b\\c\n\t\r\b\f\u0001\u001f"`, `"a\"b\\c\n\t\r\b\f\u0001\u001f"`},
		"no extra escape": {`"\/<>&\u007f"`, "\"/<>&\x7f\""},
		"key escapes":     {`{"a\nb":1}`, "{\n  \"a\\nb\": 1\n}"},
		"negative int":    {`-12`, `-12`},
		"zero":            {`0`, `0`},
		"max u64":         {`18446744073709551615`, `18446744073709551615`},
		"min i64":         {`-9223372036854775808`, `-9223372036854775808`},
		"float one":       {numOne, numOne},
		"float fraction":  {`1.50`, `1.5`},
		"exponent":        {`1e3`, `1000.0`},
		"exponent upper":  {`2E+2`, `200.0`},
		"neg exponent":    {`1e-2`, `0.01`},
		"small":           {`1e-5`, `0.00001`},
		"smaller":         {`1e-6`, `1e-6`},
		"sci mantissa":    {`1.5e-7`, `1.5e-7`},
		"big int":         {`18446744073709551616`, `1.8446744073709552e19`},
		"big neg int":     {`-9223372036854775809`, `-9.223372036854776e18`},
		"1e15":            {`1e15`, `1000000000000000.0`},
		"1e16":            {numE16, numE16},
		"decimal":         {`123.456`, `123.456`},
		"neg zero":        {`-0`, numNegZero},
		"neg zero float":  {numNegZero, numNegZero},
		"zero float":      {`0e5`, `0.0`},
		"duplicates":      {`{"a":1,"b":2,"a":3}`, "{\n  \"a\": 3,\n  \"b\": 2\n}"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := roundTrip(t, tc.in); got != tc.want {
				t.Errorf("got\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestInvalid(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"empty":            ``,
		"truncated":        `{"a":`,
		"trailing comma":   `{"a":1,}`,
		"trailing garbage": `{} x`,
		"two values":       `1 2`,
		"single quotes":    `{'a':1}`,
		"leading zero":     `01`,
		"lone surrogate":   `"\ud800"`,
		"invalid utf8":     "\"\xff\"",
		"out of range":     `1e999`,
		"too deep":         strings.Repeat("[", 200) + strings.Repeat("]", 200),
	}
	for name, src := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := jsonedit.Parse([]byte(src)); err == nil {
				t.Errorf("Parse(%q) succeeded, want an error", src)
			}
		})
	}
}

func TestEditing(t *testing.T) {
	t.Parallel()
	const src = `{"z":1,"version":"1.0.0","a":2}`
	tests := map[string]struct {
		edit func(*jsonedit.Value)
		want string
	}{
		"set existing keeps position": {
			func(v *jsonedit.Value) { v.Set("version", jsonedit.String("2.0.0")) },
			"{\n  \"z\": 1,\n  \"version\": \"2.0.0\",\n  \"a\": 2\n}",
		},
		"set new appends": {
			func(v *jsonedit.Value) { v.Set("new", jsonedit.NewBool(true)) },
			"{\n  \"z\": 1,\n  \"version\": \"1.0.0\",\n  \"a\": 2,\n  \"new\": true\n}",
		},
		"rename keeps position": {
			func(v *jsonedit.Value) {
				if !v.Rename("version", "v") {
					panic("rename failed")
				}
			},
			"{\n  \"z\": 1,\n  \"v\": \"1.0.0\",\n  \"a\": 2\n}",
		},
		"set nested value": {
			func(v *jsonedit.Value) {
				v.Set("a", jsonedit.NewObject(jsonedit.Member{Name: "x", Value: jsonedit.NewArray(jsonedit.NewInt(1))}))
			},
			"{\n  \"z\": 1,\n  \"version\": \"1.0.0\",\n  \"a\": {\n    \"x\": [\n      1\n    ]\n  }\n}",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			v, err := jsonedit.Parse([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			tc.edit(v)
			if got := string(jsonedit.Marshal(v)); got != tc.want {
				t.Errorf("got\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestAccessors(t *testing.T) {
	t.Parallel()
	v, err := jsonedit.Parse([]byte(`{"s":"x","n":1.0,"b":false,"l":[null,1],"a":1,"c":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind() != jsonedit.KindObject {
		t.Fatalf("kind = %v", v.Kind())
	}
	s, _ := v.Get("s")
	if got, ok := s.Str(); !ok || got != "x" {
		t.Errorf("Str = %q, %v", got, ok)
	}
	n, _ := v.Get("n")
	if _, ok := n.Str(); ok {
		t.Error("Str on a number reported ok")
	}
	if txt, ok := n.NumberText(); !ok || txt != numOne {
		t.Errorf("NumberText = %q, %v", txt, ok)
	}
	b, _ := v.Get("b")
	if got, ok := b.Bool(); !ok || got {
		t.Errorf("Bool = %v, %v", got, ok)
	}
	l, _ := v.Get("l")
	if els := l.Elements(); len(els) != 2 || els[0].Kind() != jsonedit.KindNull {
		t.Errorf("Elements = %v", els)
	}
	if _, ok := v.Get("missing"); ok {
		t.Error("Get(missing) reported ok")
	}
	if v.Rename("missing", "x") || v.Rename("a", "c") {
		t.Error("Rename should fail for an absent source or an existing target")
	}
	var names []string
	for _, m := range v.Members() {
		names = append(names, m.Name)
	}
	if got := strings.Join(names, ","); got != "s,n,b,l,a,c" {
		t.Errorf("member order = %s", got)
	}
}

func TestGoldenKnopeFixtures(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ base, version string }{
		"tauri.conf.json":   {"tauri", "0.2.0"},
		"package-lock.json": {"package-lock", "1.1.0"},
		"package.json":      {"package", "0.5.0"},
		"npm package.json":  {"npm", "1.1.0"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in, err := os.ReadFile(filepath.Join("testdata", tc.base+".in.json"))
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", tc.base+".out.json"))
			if err != nil {
				t.Fatal(err)
			}
			v, err := jsonedit.Parse(in)
			if err != nil {
				t.Fatal(err)
			}
			v.Set("version", jsonedit.String(tc.version))
			if name == "package-lock.json" {
				pkgs, _ := v.Get("packages")
				root, _ := pkgs.Get("")
				root.Set("version", jsonedit.String(tc.version))
			}
			got := jsonedit.Marshal(v)
			// The upstream fixtures have no trailing newline (Q10); editors and hooks may add one to the copies.
			if string(got) != strings.TrimRight(string(want), "\n") {
				t.Errorf("got\n%s\nwant\n%s", got, want)
			}
			if strings.HasSuffix(string(got), "\n") {
				t.Error("output has a trailing newline")
			}
		})
	}
}
