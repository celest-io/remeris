package tomlwrite_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/tomlwrite"
)

// nestedTables returns a document with n tables, each in the one before.
func nestedTables(n int) *tomlwrite.Doc {
	d := tomlwrite.NewDoc()
	var inner tomlwrite.Value = tomlwrite.Int(1)
	for range n {
		t := tomlwrite.NewTable()
		t.Set("k", inner)
		inner = t
	}
	d.Set("root", inner)
	return d
}

func nestedArrays(n int) *tomlwrite.Doc {
	var inner tomlwrite.Value = tomlwrite.Int(1)
	for range n {
		inner = tomlwrite.NewArray(inner)
	}
	d := tomlwrite.NewDoc()
	d.Set("k", inner)
	return d
}

func nestedInline(n int) *tomlwrite.Doc {
	var inner tomlwrite.Value = tomlwrite.Int(1)
	for range n {
		t := tomlwrite.NewInlineTable()
		t.Set("k", inner)
		inner = t
	}
	d := tomlwrite.NewDoc()
	d.Set("k", inner)
	return d
}

func TestDepthLimit(t *testing.T) {
	t.Parallel()
	selfTable := tomlwrite.NewTable()
	selfTable.Set("x", selfTable)
	selfInline := tomlwrite.NewInlineTable()
	selfInline.Set("x", selfInline)
	selfArray := tomlwrite.NewArray()
	selfArray.Append(selfArray)
	selfElements := tomlwrite.NewTable()
	selfElements.Set("x", tomlwrite.NewArrayOfTables(selfElements))

	tests := map[string]struct {
		doc     *tomlwrite.Doc
		wantErr bool
	}{
		"200 tables":                 {nestedTables(200), false},
		"255 tables":                 {nestedTables(255), false},
		"257 tables":                 {nestedTables(257), true},
		"2000 tables":                {nestedTables(2000), true},
		"200 arrays":                 {nestedArrays(200), false},
		"2000 arrays":                {nestedArrays(2000), true},
		"200 inline tables":          {nestedInline(200), false},
		"2000 inline tables":         {nestedInline(2000), true},
		"table that contains itself": {docWith("t", selfTable), true},
		"inline table cycle":         {docWith("t", selfInline), true},
		"array cycle":                {docWith("t", selfArray), true},
		"array of tables cycle":      {docWith("t", selfElements), true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, err := tc.doc.Encode()
			if tc.wantErr {
				if !errors.Is(err, tomlwrite.ErrTooDeep) || out != nil {
					t.Errorf("Encode = %d bytes, %v; want ErrTooDeep and no bytes", len(out), err)
				}
				var sb strings.Builder
				if n, err := tc.doc.WriteTo(&sb); n != 0 || err == nil || sb.Len() != 0 {
					t.Errorf("WriteTo wrote %d bytes with error %v", n, err)
				}
				return
			}
			if err != nil {
				t.Errorf("Encode: %v", err)
			}
		})
	}
}

func docWith(key string, v tomlwrite.Value) *tomlwrite.Doc {
	d := tomlwrite.NewDoc()
	d.Set(key, v)
	return d
}

// TestEveryValueHasText checks that no kind of value is ever written as nothing, which would leave `key = `.
func TestEveryValueHasText(t *testing.T) {
	t.Parallel()
	values := map[string]tomlwrite.Value{
		"string":       tomlwrite.String(""),
		"bool":         tomlwrite.Bool(false),
		"int":          tomlwrite.Int(0),
		"array":        tomlwrite.NewArray(),
		"filled array": tomlwrite.NewArray(tomlwrite.Int(1)),
		"table":        tomlwrite.NewTable(),
		"inline table": tomlwrite.NewInlineTable(),
	}
	for name, v := range values {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for where, d := range map[string]*tomlwrite.Doc{
				"key":   docWith("k", v),
				"array": docWith("k", tomlwrite.NewArray(tomlwrite.Int(1), v)),
				"inline": docWith("k", func() tomlwrite.Value {
					in := tomlwrite.NewInlineTable()
					in.Set("x", v)
					return in
				}()),
			} {
				text := encodeDoc(t, d)
				if text == "" {
					t.Errorf("%s: nothing written", where)
				}
				for line := range strings.SplitSeq(strings.TrimSuffix(text, "\n"), "\n") {
					if strings.HasSuffix(strings.TrimSpace(line), "=") {
						t.Errorf("%s: line without a value: %q", where, line)
					}
				}
			}
		})
	}
}

func TestEmptyDocument(t *testing.T) {
	t.Parallel()
	b, err := tomlwrite.NewDoc().Encode()
	if err != nil || len(b) != 0 {
		t.Errorf("Encode of an empty document = %q, %v; want no bytes and no error", b, err)
	}
}
