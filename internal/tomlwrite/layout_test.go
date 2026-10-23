package tomlwrite_test

import (
	"math"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/tomlwrite"
)

func tbl(kv ...any) *tomlwrite.Table {
	t := tomlwrite.NewTable()
	for i := 0; i+1 < len(kv); i += 2 {
		t.Set(kv[i].(string), kv[i+1].(tomlwrite.Value))
	}
	return t
}

func inl(kv ...any) *tomlwrite.InlineTable {
	t := tomlwrite.NewInlineTable()
	for i := 0; i+1 < len(kv); i += 2 {
		t.Set(kv[i].(string), kv[i+1].(tomlwrite.Value))
	}
	return t
}

func doc(kv ...any) *tomlwrite.Doc {
	d := tomlwrite.NewDoc()
	for i := 0; i+1 < len(kv); i += 2 {
		d.Set(kv[i].(string), kv[i+1].(tomlwrite.Value))
	}
	return d
}

func TestLayout(t *testing.T) {
	t.Parallel()
	one, two := tomlwrite.Int(1), tomlwrite.Int(2)
	tests := map[string]struct {
		doc  *tomlwrite.Doc
		want string
	}{
		"empty document": {doc(), ""},
		"root scalars": {
			doc("a", one, "b", tomlwrite.String("x"), "c", tomlwrite.Bool(false)),
			"a = 1\nb = \"x\"\nc = false\n",
		},
		"scalars before sub-tables": {
			doc("t", tbl("sub", tbl("x", one), "a", one, "b", two)),
			"[t]\na = 1\nb = 2\n\n[t.sub]\nx = 1\n",
		},
		"root scalar set after a table still comes first": {
			doc("t", tbl("b", two), "a", one),
			"a = 1\n\n[t]\nb = 2\n",
		},
		"blank line between sections only": {
			doc("a", tbl("x", one), "b", tbl("y", two)),
			"[a]\nx = 1\n\n[b]\ny = 2\n",
		},
		"implicit parent": {
			doc("packages", tbl("first", tbl("k", one))),
			"[packages.first]\nk = 1\n",
		},
		"implicit parents, three deep": {
			doc("a", tbl("b", tbl("c", tbl("k", one)))),
			"[a.b.c]\nk = 1\n",
		},
		"parent with a body keeps its header": {
			doc("a", tbl("k", one, "b", tbl("l", two))),
			"[a]\nk = 1\n\n[a.b]\nl = 2\n",
		},
		"scoped package name": {
			doc(
				"packages",
				tbl("@scope/first-package", tbl("versioned_files", tomlwrite.NewArray(tomlwrite.String("package.json")))),
			),
			"[packages.\"@scope/first-package\"]\nversioned_files = [\"package.json\"]\n",
		},
		"key with a dot is one segment": {
			doc("a", tbl("b.c", tbl("k", one))),
			"[a.\"b.c\"]\nk = 1\n",
		},
		"empty table": {
			doc("a", tbl()),
			"[a]\n",
		},
		"empty table below an implicit parent": {
			doc("a", tbl("b", tbl())),
			"[a.b]\n",
		},
		"empty table among others": {
			doc("x", tbl("k", one), "e", tbl(), "y", tbl("k", two)),
			"[x]\nk = 1\n\n[e]\n\n[y]\nk = 2\n",
		},
		"sibling sub-tables of an implicit parent": {
			doc("a", tbl("b", tbl("k", one), "c", tbl("k", two)), "d", tbl("k", one)),
			"[a.b]\nk = 1\n\n[a.c]\nk = 2\n\n[d]\nk = 1\n",
		},
		"array of tables": {
			doc("a", tomlwrite.NewArrayOfTables(tbl("x", one), tbl("x", two))),
			"[[a]]\nx = 1\n\n[[a]]\nx = 2\n",
		},
		"array of tables is also found by its elements": {
			doc("a", tomlwrite.NewArray(tbl("x", one), tbl("x", two))),
			"[[a]]\nx = 1\n\n[[a]]\nx = 2\n",
		},
		"array of empty tables": {
			doc("a", tomlwrite.NewArrayOfTables(tbl(), tbl())),
			"[[a]]\n\n[[a]]\n",
		},
		"array of tables after root scalars": {
			doc("a", tomlwrite.NewArrayOfTables(tbl("x", one)), "k", one),
			"k = 1\n\n[[a]]\nx = 1\n",
		},
		"sub-table of an array element": {
			doc("a", tomlwrite.NewArrayOfTables(tbl("sub", tbl("y", two), "x", one))),
			"[[a]]\nx = 1\n\n[a.sub]\ny = 2\n",
		},
		"array of tables in an array element": {
			doc("a", tomlwrite.NewArrayOfTables(tbl("b", tomlwrite.NewArrayOfTables(tbl("z", one), tbl("z", two))))),
			"[[a]]\n\n[[a.b]]\nz = 1\n\n[[a.b]]\nz = 2\n",
		},
		"steps of workflows": {
			doc("workflows", tomlwrite.NewArrayOfTables(
				tbl("name", tomlwrite.String("w1"), "steps", tomlwrite.NewArrayOfTables(tbl("type", tomlwrite.String("A")))),
				tbl("name", tomlwrite.String("w2")),
			)),
			"[[workflows]]\nname = \"w1\"\n\n[[workflows.steps]]\ntype = \"A\"\n\n[[workflows]]\nname = \"w2\"\n",
		},
		"mixed array stays inline": {
			doc(
				"k",
				tomlwrite.NewArray(
					inl("path", tomlwrite.String("x"), "dependency", tomlwrite.String("y")),
					tomlwrite.String("z"),
				),
			),
			"k = [{ path = \"x\", dependency = \"y\" }, \"z\"]\n",
		},
		"tables in a mixed array are inline": {
			doc("k", tomlwrite.NewArray(one, tbl("a", one))),
			"k = [1, { a = 1 }]\n",
		},
		"inline tables only stay inline": {
			doc("k", tomlwrite.NewArray(inl("a", one), inl("a", two))),
			"k = [{ a = 1 }, { a = 2 }]\n",
		},
		"empty array": {
			doc("k", tomlwrite.NewArray()),
			"k = []\n",
		},
		"empty array of tables": {
			doc("k", tomlwrite.NewArrayOfTables()),
			"k = []\n",
		},
		"inline array": {
			doc("k", tomlwrite.NewArray(one, two, tomlwrite.Int(3))),
			"k = [1, 2, 3]\n",
		},
		"nested arrays": {
			doc("k", tomlwrite.NewArray(tomlwrite.NewArray(one, two), tomlwrite.NewArray())),
			"k = [[1, 2], []]\n",
		},
		"array of arrays of tables is inline": {
			doc("k", tomlwrite.NewArray(tomlwrite.NewArrayOfTables(tbl("a", one)))),
			"k = [[{ a = 1 }]]\n",
		},
		"inline table": {
			doc("k", inl("a", tomlwrite.String("b"), "c", tomlwrite.String("d"))),
			"k = { a = \"b\", c = \"d\" }\n",
		},
		"empty inline table": {
			doc("k", inl()),
			"k = {}\n",
		},
		"inline table in a table": {
			doc("t", tbl("k", inl("a", one), "s", tbl("x", one))),
			"[t]\nk = { a = 1 }\n\n[t.s]\nx = 1\n",
		},
		"nested inline tables": {
			doc("k", inl("a", inl("b", one), "t", tbl("c", two), "l", tomlwrite.NewArray(one))),
			"k = { a = { b = 1 }, t = { c = 2 }, l = [1] }\n",
		},
		"table in an inline table has no section": {
			doc("k", inl("t", tbl("c", tbl("d", one)))),
			"k = { t = { c = { d = 1 } } }\n",
		},
		"integers": {
			doc(
				"min",
				tomlwrite.Int(math.MinInt64),
				"max",
				tomlwrite.Int(math.MaxInt64),
				"neg",
				tomlwrite.Int(-7),
				"zero",
				tomlwrite.Int(0),
			),
			"min = -9223372036854775808\nmax = 9223372036854775807\nneg = -7\nzero = 0\n",
		},
		"booleans": {
			doc("t", tomlwrite.Bool(true), "f", tomlwrite.Bool(false)),
			"t = true\nf = false\n",
		},
		"set again keeps the position": {
			func() *tomlwrite.Doc {
				d := doc("a", one, "b", one, "c", one)
				d.Set("a", two)
				return d
			}(),
			"a = 2\nb = 1\nc = 1\n",
		},
		"set nil removes the key": {
			func() *tomlwrite.Doc {
				d := doc("a", one, "b", one, "c", one)
				d.Set("b", nil)
				d.Set("nothing", nil)
				d.Set("typed", (*tomlwrite.Table)(nil))
				return d
			}(),
			"a = 1\nc = 1\n",
		},
		"nil items are left out of arrays": {
			doc("k", tomlwrite.NewArray(one, nil, (*tomlwrite.Array)(nil), two)),
			"k = [1, 2]\n",
		},
		"hostile header in a key": {
			doc("x]\n[evil", tbl("k", one)),
			"[\"x]\\n[evil\"]\nk = 1\n",
		},
		"hostile string": {
			doc("k", tomlwrite.String("x\"]\n[evil]\nk = \"v")),
			"k = \"\"\"\nx\"]\n[evil]\nk = \"v\"\"\"\n",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := encodeDoc(t, tc.doc); got != tc.want {
				t.Errorf("String()\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

func TestEndsWithOneNewline(t *testing.T) {
	t.Parallel()
	docs := map[string]*tomlwrite.Doc{
		"scalar":         doc("a", tomlwrite.Int(1)),
		"section":        doc("a", tbl()),
		"array of table": doc("a", tomlwrite.NewArrayOfTables(tbl(), tbl("k", tomlwrite.Int(1)))),
		"multi-line":     doc("a", tomlwrite.String("x\n")),
		"inline":         doc("a", inl("k", tomlwrite.Int(1))),
	}
	for name, d := range docs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := encodeDoc(t, d)
			if !strings.HasSuffix(got, "\n") || strings.HasSuffix(got, "\n\n") || strings.HasPrefix(got, "\n") {
				t.Errorf("String() = %q, want one final line feed and no leading one", got)
			}
		})
	}
}

func TestStableOrder(t *testing.T) {
	t.Parallel()
	build := func() *tomlwrite.Doc {
		d := tomlwrite.NewDoc()
		for _, k := range []string{"z", "m", "a", "k", "b"} {
			d.Set(k, tbl("s", tomlwrite.String(k), "n", tbl("x", tomlwrite.Int(1))))
		}
		return d
	}
	want := encodeDoc(t, build())
	for range 50 {
		if got := encodeDoc(t, build()); got != want {
			t.Fatalf("output changed between runs:\n%s\n---\n%s", got, want)
		}
	}
	if !strings.HasPrefix(want, "[z]\n") {
		t.Errorf("keys are not in insertion order:\n%s", want)
	}
}

func TestAccessors(t *testing.T) {
	t.Parallel()
	d := doc("a", tomlwrite.Int(1), "b", tomlwrite.Int(2))
	if d.Len() != 2 {
		t.Errorf("Len = %d, want 2", d.Len())
	}
	if v, ok := d.Get("b"); !ok || v != tomlwrite.Int(2) {
		t.Errorf("Get(b) = %v, %v", v, ok)
	}
	if _, ok := d.Get("c"); ok {
		t.Error("Get(c) found a key that was never set")
	}
	got := d.Entries()
	got[0].Key = "changed"
	if d.Entries()[0].Key != "a" {
		t.Error("Entries returned the internal slice")
	}
	a := tomlwrite.NewArray(tomlwrite.Int(1))
	items := a.Items()
	items[0] = tomlwrite.Int(9)
	if a.Len() != 1 || a.Items()[0] != tomlwrite.Int(1) {
		t.Error("Items returned the internal slice")
	}
}

// encodeDoc encodes d and fails the test on an error.
func encodeDoc(t *testing.T, d *tomlwrite.Doc) string {
	t.Helper()
	b, err := d.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return string(b)
}

func TestWriteTo(t *testing.T) {
	t.Parallel()
	d := doc("a", tbl("k", tomlwrite.String("v")))
	var sb strings.Builder
	n, err := d.WriteTo(&sb)
	if err != nil {
		t.Fatal(err)
	}
	if sb.String() != encodeDoc(t, d) || n != int64(sb.Len()) {
		t.Errorf("WriteTo wrote %q (%d bytes), want %q", sb.String(), n, encodeDoc(t, d))
	}
}
