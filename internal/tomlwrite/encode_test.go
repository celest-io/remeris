package tomlwrite

import (
	"errors"
	"strings"
	"testing"
)

func TestEncodeString(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		in   string
		want string
	}{
		"plain":                 {"hello", `"hello"`},
		"empty":                 {"", `""`},
		"double quote":          {`say "hi"`, `'say "hi"'`},
		"single quote":          {`it's`, `"it's"`},
		"both quotes":           {`say "it's"`, `"""say "it's""""`},
		"backslash":             {`a\b`, `'a\b'`},
		"backslash and double":  {`a\"b`, `'a\"b'`},
		"backslash both quotes": {`a\"'b`, `'''a\"'b'''`},
		"newline":               {"a\nb", "\"\"\"\na\nb\"\"\""},
		"trailing newline":      {"x\n", "\"\"\"\nx\n\"\"\""},
		"leading newline":       {"\nx", "\"\"\"\n\nx\"\"\""},
		"newline and backslash": {"a\\\nb", "'''\na\\\nb'''"},
		"tab":                   {"a\tb", `"a\tb"`},
		"control":               {"a\x01b", `"a\u0001b"`},
		"nul":                   {"a\x00b", `"a\u0000b"`},
		"backspace":             {"a\bb", `"a\bb"`},
		"form feed":             {"a\fb", `"a\fb"`},
		"carriage return":       {"a\rb", `"a\rb"`},
		"crlf":                  {"a\r\nb", "\"\"\"\na\\r\nb\"\"\""},
		"control and newline":   {"a\x01\nb", "\"\"\"\na\\u0001\nb\"\"\""},
		"delete":                {"a\x7fb", `"a\u007Fb"`},
		"unicode":               {"héllo ✓ 日本", `"héllo ✓ 日本"`},
		"c1 control":            {"a\u0085b", "\"a\u0085b\""},
		"double quote run":      {`a"""b`, `'a"""b'`},
		"double run, single":    {`a"""b'c`, `'''a"""b'c'''`},
		"single run, double":    {`a'''b"`, `"""a'''b""""`},
		"both runs":             {`a"""b'''c`, `"a\"\"\"b'''c"`},
		"both runs and newline": {"a\"\"\"\nb'''c", "\"\"\"\na\"\"\\\"\nb'''c\"\"\""},
		"long run of quotes":    {`""""""`, `'""""""'`},
		"long run, ml basic":    {"\"\"\"\"\"\"'''\n", "\"\"\"\n\"\"\\\"\"\"\\\"'''\n\"\"\""},
		"quote only":            {`"`, `'"'`},
		"quote then single":     {`"'`, `""""'"""`},
		"control and quote":     {"\x01\"", `"\u0001\""`},
		"control, ml, 3 quotes": {"\x01\"\"\"\n", "\"\"\"\n\\u0001\"\"\\\"\n\"\"\""},
		"very long":             {strings.Repeat("x", 100000), `"` + strings.Repeat("x", 100000) + `"`},
		"very long with quotes": {strings.Repeat(`"`, 5000), `'` + strings.Repeat(`"`, 5000) + `'`},
		"version variable": {
			`git commit -m "chore: prepare release $version"`,
			`'git commit -m "chore: prepare release $version"'`,
		},
		"hostile table header":         {"x\"]\n[evil]\nk = \"v", "\"\"\"\nx\"]\n[evil]\nk = \"v\"\"\""},
		"hostile with both":            {"x\"\"\"\n[evil]\n'''", "\"\"\"\nx\"\"\\\"\n[evil]\n'''\"\"\""},
		"hostile multiline end":        {"\"\"\"\nevil = 1\n\"\"\"", "'''\n\"\"\"\nevil = 1\n\"\"\"'''"},
		"hostile literal end":          {"'\n[evil]", "\"\"\"\n'\n[evil]\"\"\""},
		"hostile escape at end":        {`x\`, `'x\'`},
		"hostile escape in ml":         {"x\\\n  y'\"", "'''\nx\\\n  y'\"'''"},
		"starts with double":           {`"x`, `'"x'`},
		"starts with double, ml":       {`"x'`, `""""x'"""`},
		"starts with two doubles":      {`""x'`, `"""""x'"""`},
		"starts with single":           {`'x"`, `"""'x""""`},
		"starts with two singles":      {`''x"`, `"""''x""""`},
		"starts with single, esc":      {`'x"\`, `'''` + `'x"\` + `'''`},
		"starts with two singles, esc": {`''x"\`, `'''` + `''x"\` + `'''`},
		"ends with backslash, ml":      {"x\\\n'\"\\", "'''\nx\\\n'\"\\'''"},
		"ends with backslash":          {"a'\"\\", `'''a'"\'''`},
		"ends with two singles":        {"a''\"\\", `'''a''"\'''`},
		"hostile unicode escape":       {`"]`, `'"]'`},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := encodeString(tc.in)
			if err != nil {
				t.Fatalf("encodeString(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("encodeString(%q)\n got: %s\nwant: %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestEncodeKey(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		in   string
		want string
	}{
		"bare":           {"abc-1_X", "abc-1_X"},
		"digits":         {"123", "123"},
		"dotted":         {"a.b", `"a.b"`},
		"space":          {"with space", `"with space"`},
		"double quote":   {`has"quote`, `'has"quote'`},
		"single quote":   {"it's", `"it's"`},
		"both quotes":    {`a"b'c`, `"a\"b'c"`},
		"backslash":      {`a\b`, `'a\b'`},
		"backslash both": {`a\"b'`, `"a\\\"b'"`},
		"empty":          {"", `""`},
		"unicode":        {"é", `"é"`},
		"scoped name":    {"@scope/name", `"@scope/name"`},
		"newline":        {"a\nb", `"a\nb"`},
		"newline quote":  {"a\n\"", `"a\n\""`},
		"tab":            {"a\tb", `"a\tb"`},
		"tab and quote":  {"a\t\"", "'a\t\"'"},
		"control":        {"a\x01'", `"a\u0001'"`},
		"delete":         {"a\x7f", `"a\u007F"`},
		"tab and single": {"a\t'", `"a\t'"`},
		"tab, both":      {"a\t'\"", `"a\t'\""`},
		"delete and dq":  {"\x7f\"", `"\u007F\""`},
		"c1 control":     {"a\u0085", "\"a\u0085\""},
		"bom":            {"\ufeffa", "\"\ufeffa\""},
		"brackets":       {"[x]", `"[x]"`},
		"equals":         {"a = b", `"a = b"`},
		"hash":           {"a#b", `"a#b"`},
		"hostile header": {"x]\n[evil", `"x]\n[evil"`},
		"hostile quote":  {"x\"]\n[evil]\n\"", `"x\"]\n[evil]\n\""`},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := encodeKey(tc.in)
			if err != nil {
				t.Fatalf("encodeKey(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("encodeKey(%q)\n got: %s\nwant: %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestInvalidUTF8(t *testing.T) {
	t.Parallel()
	bad := "a\xffb"
	tests := map[string]func() *Doc{
		"value": func() *Doc {
			d := NewDoc()
			d.Set("k", String(bad))
			return d
		},
		"key": func() *Doc {
			d := NewDoc()
			d.Set(bad, String("v"))
			return d
		},
		"section key": func() *Doc {
			d := NewDoc()
			tbl := NewTable()
			tbl.Set("k", Int(1))
			d.Set(bad, tbl)
			return d
		},
		"inline key": func() *Doc {
			d := NewDoc()
			in := NewInlineTable()
			in.Set(bad, Int(1))
			d.Set("k", in)
			return d
		},
		"array item": func() *Doc {
			d := NewDoc()
			d.Set("k", NewArray(String("ok"), String(bad)))
			return d
		},
	}
	for name, build := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d := build()
			if _, err := d.Encode(); !errors.Is(err, ErrInvalidUTF8) {
				t.Errorf("Encode error = %v, want ErrInvalidUTF8", err)
			}
			var sb strings.Builder
			if n, err := d.WriteTo(&sb); n != 0 || err == nil || sb.Len() != 0 {
				t.Errorf("WriteTo wrote %d bytes (%q) with error %v", n, sb.String(), err)
			}
		})
	}
}
