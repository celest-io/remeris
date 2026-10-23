package tomlwrite_test

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/celest-io/remeris/internal/tomlwrite"
)

// These tests check the one property that matters for text taken from untrusted files: whatever the document holds,
// the output is valid TOML and a TOML parser reads exactly the document back. A string or a key that could end a
// quote, a header or a line would change the parsed structure and fail the comparison.

// chooser picks numbers: a seeded generator in the property test, the fuzzer's bytes in the fuzz target.
type chooser interface {
	intn(n int) int
}

// randChooser is a splitmix64 generator: deterministic from its seed, and good enough to vary test data.
type randChooser struct{ state uint64 }

func (c *randChooser) next() uint64 {
	c.state += 0x9e3779b97f4a7c15
	z := c.state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

func (c *randChooser) intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(c.next()>>33) % n
}

// byteChooser reads the input and answers 0 when it runs out, which always picks the smallest choice.
type byteChooser struct {
	data []byte
	pos  int
}

func (c *byteChooser) intn(n int) int {
	if c.pos >= len(c.data) {
		return 0
	}
	b := int(c.data[c.pos])
	c.pos++
	return b % n
}

// fragments are the pieces hostile text is made of.
var fragments = []string{
	"x", "", "a.b", " ", `"`, `'`, `\`, "\n", "\r", "\r\n", "\t", "\x00", "\x01", "\b", "\f", "\x1f", "\x7f", `"""`, `'''`,
	`""`, `''`, "]", "[", "[evil]", "]]", "[[evil]]", "=", " = ", "#", "{", "}", ",", "é", "日本", "\u0085", "\u2028",
	"\ufeff", "\U0001F600", `\u0022`, `\n`, "\\\n", "k = \"v\"", "\"]\n[evil]\nk = \"v", "'''\n[evil]\n'''", "@scope/name",
}

func genString(c chooser) string {
	var sb strings.Builder
	for range c.intn(7) {
		sb.WriteString(fragments[c.intn(len(fragments))])
	}
	return sb.String()
}

func genInt(c chooser) tomlwrite.Int {
	switch c.intn(8) {
	case 0:
		return math.MinInt64
	case 1:
		return math.MaxInt64
	default:
		return tomlwrite.Int(c.intn(2000) - 1000)
	}
}

const maxDepth = 4

func genValue(c chooser, depth int) tomlwrite.Value {
	kinds := 6
	if depth >= maxDepth {
		kinds = 3
	}
	switch c.intn(kinds) {
	case 0:
		return tomlwrite.String(genString(c))
	case 1:
		return tomlwrite.Bool(c.intn(2) == 1)
	case 2:
		return genInt(c)
	case 3:
		return genArray(c, depth+1)
	case 4:
		return genTable(c, depth+1)
	default:
		return genInline(c, depth+1)
	}
}

func genArray(c chooser, depth int) *tomlwrite.Array {
	a := tomlwrite.NewArray()
	n := c.intn(4)
	if c.intn(3) == 0 { // an array of tables, written as [[sections]]
		for range n {
			a.Append(genTable(c, depth+1))
		}
		return a
	}
	for range n {
		a.Append(genValue(c, depth+1))
	}
	return a
}

func genTable(c chooser, depth int) *tomlwrite.Table {
	t := tomlwrite.NewTable()
	for range c.intn(4) {
		t.Set(genString(c), genValue(c, depth))
	}
	return t
}

func genInline(c chooser, depth int) *tomlwrite.InlineTable {
	t := tomlwrite.NewInlineTable()
	for range c.intn(4) {
		t.Set(genString(c), genValue(c, depth))
	}
	return t
}

func genDoc(c chooser) *tomlwrite.Doc {
	d := tomlwrite.NewDoc()
	for range c.intn(6) {
		d.Set(genString(c), genValue(c, 0))
	}
	return d
}

// expected is the value a TOML parser must produce for v.
func expected(v tomlwrite.Value) any {
	switch v := v.(type) {
	case tomlwrite.String:
		return string(v)
	case tomlwrite.Bool:
		return bool(v)
	case tomlwrite.Int:
		return int64(v)
	case *tomlwrite.Array:
		items := []any{}
		for _, item := range v.Items() {
			items = append(items, expected(item))
		}
		return items
	case *tomlwrite.Table:
		return expectedMap(v.Entries())
	case *tomlwrite.InlineTable:
		return expectedMap(v.Entries())
	}
	panic(fmt.Sprintf("unexpected value %T", v))
}

func expectedMap(entries []tomlwrite.Entry) map[string]any {
	m := map[string]any{}
	for _, e := range entries {
		m[e.Key] = expected(e.Value)
	}
	return m
}

func checkRoundTrip(t *testing.T, d *tomlwrite.Doc) {
	t.Helper()
	text, err := d.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !strings.HasSuffix(text, "\n") && text != "" {
		t.Errorf("output does not end with a line feed: %q", text)
	}
	var got map[string]any
	if err := toml.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("the output is not valid TOML: %v\n%s", err, text)
	}
	want := expectedMap(d.Entries())
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed document differs\n got: %#v\nwant: %#v\ntext:\n%s", got, want, text)
	}
}

func TestRoundTripRandom(t *testing.T) {
	t.Parallel()
	for seed := range uint64(3000) {
		c := &randChooser{state: seed}
		checkRoundTrip(t, genDoc(c))
		if t.Failed() {
			t.Fatalf("seed %d", seed)
		}
	}
}

func TestRoundTripHostile(t *testing.T) {
	t.Parallel()
	hostile := []string{
		"x\"]\n[evil]\nk = \"v",
		"x\"\"\"\n[evil]\nk = 1\n\"\"\"",
		"x'''\n[evil]\n'''",
		"\\",
		"\\\n[evil]",
		"a\"\"\"\"\"\"b'''c",
		"\"",
		"\"\"",
		"'\"",
		"ends with quote\"",
		"ends with two quotes\"\"",
		"ends with single'",
		"ends with two singles''",
		"\n",
		"\r\n",
		"\x00\x01\x7f",
		"\\u0022]\n[evil]",
		"]]\n[[evil]]",
	}
	for _, h := range hostile {
		t.Run(fmt.Sprintf("%q", h), func(t *testing.T) {
			t.Parallel()
			d := tomlwrite.NewDoc()
			d.Set("value", tomlwrite.String(h))
			d.Set("array", tomlwrite.NewArray(tomlwrite.String(h), tomlwrite.String(h)))
			d.Set("inline", func() tomlwrite.Value {
				in := tomlwrite.NewInlineTable()
				in.Set(h, tomlwrite.String(h))
				return in
			}())
			section := tomlwrite.NewTable()
			section.Set(h, tomlwrite.String(h))
			sub := tomlwrite.NewTable()
			sub.Set(h, tomlwrite.Int(1))
			section.Set(h+"!", sub)
			d.Set(h, section)
			d.Set(h+"?", tomlwrite.NewArrayOfTables(section, section))
			checkRoundTrip(t, d)
		})
	}
}

func FuzzRoundTrip(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("\x05\x01\x02\x03\x04\x05\x06\x07"))
	f.Add([]byte("hostile \"]\n[evil]\nk = \"v\x04\x04\x04\x03\x03"))
	for seed := range uint64(20) {
		c := &randChooser{state: seed + 1000}
		var data []byte
		for range 200 {
			data = append(data, byte(c.next()&0xff))
		}
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		checkRoundTrip(t, genDoc(&byteChooser{data: data}))
	})
}

// FuzzString checks any valid UTF-8 text as a value and as a key.
func FuzzString(f *testing.F) {
	for _, s := range fragments {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		s = strings.ToValidUTF8(s, "\uFFFD")
		d := tomlwrite.NewDoc()
		d.Set("v", tomlwrite.String(s))
		d.Set("a", tomlwrite.NewArray(tomlwrite.String(s)))
		section := tomlwrite.NewTable()
		section.Set(s, tomlwrite.String(s))
		d.Set(s, section)
		checkRoundTrip(t, d)
	})
}
