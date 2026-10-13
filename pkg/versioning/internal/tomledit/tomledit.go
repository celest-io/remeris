// Package tomledit is a small format-preserving TOML editor.
//
// It parses TOML bytes into an index of values with byte spans and resolves values by key path. Edits splice bytes at
// those spans, so every byte outside the edited span (comments, whitespace, key order, quoting style) is unchanged.
// It is the Go counterpart of the parts of toml_edit that Knope uses for Cargo.toml, Cargo.lock, gleam.toml and
// pyproject.toml.
//
// A key path addresses the same value however TOML spells it: [a.b] headers, dotted keys (a.b.c = 1), inline tables
// (a = { b = { c = 1 } }), quoted keys ("a.b") and arrays of tables ([[package]], addressed by index).
//
// The package is pure: it reads and writes byte slices only.
package tomledit

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// ErrSyntax is wrapped by every error that reports invalid TOML.
var ErrSyntax = errors.New("invalid TOML")

// Kind is the type of a TOML value or table.
type Kind int

// The kinds of values and tables.
const (
	// String is a basic, literal or multi-line string.
	String Kind = iota
	// Integer is an integer.
	Integer
	// Float is a floating point number.
	Float
	// Bool is true or false.
	Bool
	// Datetime is an offset or local date-time, date or time.
	Datetime
	// Array is an inline array, such as [1, 2].
	Array
	// InlineTable is a table written as { k = v }.
	InlineTable
	// Table is a table defined by a [header], by dotted keys or implied by a longer header.
	Table
	// ArrayOfTables is a [[header]] array.
	ArrayOfTables
)

// Key is one segment of a key path. Index selects the element when the segment names an array of tables
// ([[package]]); it is ignored for every other kind of value.
type Key struct {
	Name  string
	Index int
}

// Keys turns plain names into a key path whose array-of-tables indexes are all 0.
func Keys(names ...string) []Key {
	keys := make([]Key, len(names))
	for i, name := range names {
		keys[i] = Key{Name: name}
	}
	return keys
}

// Value describes what a lookup found.
type Value struct {
	Kind Kind
	// Str is the decoded text of a String. For Integer, Float and Datetime it is the literal as written.
	Str string
	// Int is the value of an Integer.
	Int int64
	// Bool is the value of a Bool.
	Bool bool
	// Span is the half-open byte range [start, end) of the raw value, including quotes, brackets and braces.
	// It is zero for Table and ArrayOfTables, which have no single value span.
	Span [2]int
}

// node is one entry of the parsed index.
type node struct {
	val   Value
	kids  map[string]*node
	elems []*node // elements of an array of tables
	// header is true for the root, tables written as [header] and array-of-tables elements: the tables that accept
	// inserted keys. dotted marks tables created by dotted keys, which may not be redefined by a header.
	header, dotted bool
	// lastEnd is where a key inserted into a header table goes: the end of the line of its last key-value
	// pair, or of its header line. For a table that dotted keys define it is the end of the line of the last
	// pair that belongs to it (nested dotted keys included). For an inline table it is the end of the last pair.
	lastEnd int
	// prefix is, for a table that dotted keys define in a header table or the root, the key text that reaches it
	// from there, as the first dotted key wrote it ("a", "'a.b'" or "dependencies.a "), without the final dot.
	// It is empty for dotted tables inside inline tables, which cannot take inserted keys.
	prefix string
}

func newTable() *node { return &node{val: Value{Kind: Table}, kids: map[string]*node{}} }

// Document is a parsed TOML document that can be edited.
type Document struct {
	src  []byte
	root *node
}

// bom is the UTF-8 byte order mark, which some editors put at the start of a file.
var bom = []byte{0xEF, 0xBB, 0xBF}

// bomLen returns the length of the byte order mark at the start of src, or 0.
func bomLen(src []byte) int {
	if bytes.HasPrefix(src, bom) {
		return len(bom)
	}
	return 0
}

// withoutBOM returns src as the TOML parsers can read it. They do not take a byte order mark, so it is replaced by
// as many spaces: whitespace is valid at the start of a document and keeps every offset equal to the offset in src.
// src itself is not changed.
func withoutBOM(src []byte) []byte {
	if !bytes.HasPrefix(src, bom) {
		return src
	}
	view := slices.Clone(src)
	copy(view, "   ")
	return view
}

// Parse parses src, which must be valid TOML. The error wraps ErrSyntax.
//
// A UTF-8 byte order mark at the start is accepted, as toml_edit does. Spans and error positions are offsets in
// src, so they count the mark, and Bytes returns it unchanged.
func Parse(src []byte) (*Document, error) {
	src = slices.Clone(src)
	// The unstable parser used by index does not validate every scalar (it accepts 0=0000-), so a full parse
	// decides validity.
	if err := toml.Unmarshal(withoutBOM(src), &map[string]any{}); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSyntax, err)
	}
	root, err := index(src)
	if err != nil {
		return nil, err
	}
	return &Document{src: src, root: root}, nil
}

// Bytes returns the current content of the document.
func (d *Document) Bytes() []byte { return slices.Clone(d.src) }

// resolve walks path. With strict, the Index of a Key selects an array-of-tables element; otherwise the last
// element is used, as TOML does for headers. A path that ends on an array of tables returns the array itself,
// unless elem is true.
func (d *Document) resolve(path []Key, strict, elem bool) (*node, bool) {
	cur := d.root
	for i, k := range path {
		if cur.val.Kind != Table && cur.val.Kind != InlineTable {
			return nil, false
		}
		kid, ok := cur.kids[k.Name]
		if !ok {
			return nil, false
		}
		if kid.val.Kind == ArrayOfTables && (i < len(path)-1 || elem) {
			idx := len(kid.elems) - 1
			if strict {
				idx = k.Index
			}
			if idx < 0 || idx >= len(kid.elems) {
				return nil, false
			}
			kid = kid.elems[idx]
		}
		cur = kid
	}
	return cur, true
}

// Lookup finds the value at the key path made of the given names. Every name is one literal key (so "a.b"
// is a single quoted key, not a dotted path). Where a name is an array of tables the last element is used,
// except at the end of the path, where Lookup returns the array of tables itself.
func (d *Document) Lookup(path ...string) (Value, bool) {
	n, ok := d.resolve(Keys(path...), false, false)
	if !ok {
		return Value{}, false
	}
	return n.val, true
}

// LookupIn is Lookup with array-of-tables indexes. Out-of-range indexes are not found.
func (d *Document) LookupIn(path []Key) (Value, bool) {
	n, ok := d.resolve(path, true, false)
	if !ok {
		return Value{}, false
	}
	return n.val, true
}

// ArrayOfTablesLen returns the number of elements of the array of tables at path, or 0 if there is none.
func (d *Document) ArrayOfTablesLen(path ...string) int {
	n, ok := d.resolve(Keys(path...), false, false)
	if !ok || n.val.Kind != ArrayOfTables {
		return 0
	}
	return len(n.elems)
}

// index parses src into a tree of nodes with spans, which are offsets in src even when it starts with a byte
// order mark. The Document edits call it again after each splice.
func index(src []byte) (*node, error) {
	p := unstable.Parser{}
	p.Reset(withoutBOM(src))
	root := newTable()
	root.header = true
	root.lastEnd = bomLen(src) // a key inserted into an empty root goes after the mark
	cur := root
	for p.NextExpression() {
		expr := p.Expression()
		var err error
		switch expr.Kind {
		case unstable.Table, unstable.ArrayTable:
			cur, err = addHeader(root, expr, src)
		case unstable.KeyValue:
			var dotted []*node
			dotted, err = addPair(cur, expr, src)
			if err == nil {
				cur.lastEnd = lineEnd(src, rawEnd(expr))
				for _, t := range dotted {
					t.lastEnd = cur.lastEnd
				}
			}
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrSyntax, err)
		}
	}
	if err := p.Error(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSyntax, err)
	}
	return root, nil
}

func rawEnd(n *unstable.Node) int { return int(n.Raw.Offset + n.Raw.Length) }

// lineEnd is the offset just after the line break that ends the line containing i, or len(src).
func lineEnd(src []byte, i int) int {
	if j := slices.Index(src[i:], '\n'); j >= 0 {
		return i + j + 1
	}
	return len(src)
}

// keyNames returns the decoded names of a key and the offset just after the last one.
func keyNames(it unstable.Iterator) (names []string, end int) {
	names, _, end = keyParts(it)
	return names, end
}

// keyParts is keyNames that also returns, for each part, the raw start offset and the offset just after it.
func keyParts(it unstable.Iterator) (names []string, spans [][2]int, end int) {
	for it.Next() {
		names = append(names, string(it.Node().Data))
		spans = append(spans, [2]int{int(it.Node().Raw.Offset), rawEnd(it.Node())})
		end = rawEnd(it.Node())
	}
	return names, spans, end
}

// addHeader records a [table] or [[array]] header and returns the table that the pairs after it belong to.
func addHeader(root *node, expr *unstable.Node, src []byte) (*node, error) {
	names, end := keyNames(expr.Key())
	cur := root
	for _, name := range names[:len(names)-1] {
		kid, ok := cur.kids[name]
		switch {
		case !ok:
			kid = newTable()
			cur.kids[name] = kid
		case kid.val.Kind == ArrayOfTables:
			kid = kid.elems[len(kid.elems)-1]
		case kid.val.Kind != Table:
			return nil, fmt.Errorf("key %q is not a table", name)
		}
		cur = kid
	}
	last := names[len(names)-1]
	kid, ok := cur.kids[last]
	if expr.Kind == unstable.ArrayTable {
		if !ok {
			kid = &node{val: Value{Kind: ArrayOfTables}}
			cur.kids[last] = kid
		} else if kid.val.Kind != ArrayOfTables {
			return nil, fmt.Errorf("key %q is not an array of tables", last)
		}
		elem := newTable()
		elem.header = true
		elem.lastEnd = lineEnd(src, end)
		kid.elems = append(kid.elems, elem)
		return elem, nil
	}
	if !ok {
		kid = newTable()
		cur.kids[last] = kid
	} else if kid.val.Kind != Table || kid.header || kid.dotted {
		return nil, fmt.Errorf("duplicate key %q", last)
	}
	kid.header = true
	kid.lastEnd = lineEnd(src, end)
	return kid, nil
}

// addPair records a key-value pair, dotted or not, in table. It returns the tables that the dotted part of the
// key goes through, outermost first.
func addPair(table *node, kv *unstable.Node, src []byte) ([]*node, error) {
	names, spans, end := keyParts(kv.Key())
	var dotted []*node
	owned := table.header
	for i, name := range names[:len(names)-1] {
		kid, ok := table.kids[name]
		if !ok {
			kid = newTable()
			kid.dotted = true
			if owned {
				kid.prefix = string(src[spans[0][0]:skipSpace(src, spans[i][1])])
			}
			table.kids[name] = kid
		}
		if kid.val.Kind != Table || kid.header {
			return nil, fmt.Errorf("key %q is not a table that dotted keys can extend", name)
		}
		dotted = append(dotted, kid)
		table = kid
	}
	last := names[len(names)-1]
	if _, dup := table.kids[last]; dup {
		return nil, fmt.Errorf("duplicate key %q", last)
	}
	n, err := valueNode(kv.Value(), end, rawEnd(kv), src)
	if err != nil {
		return nil, err
	}
	table.kids[last] = n
	return dotted, nil
}

// valueNode builds the node of a value. The parser reports no range for arrays and only the opening brace of an
// inline table, so the value starts at the first byte after "=" and ends where its key-value pair ends.
func valueNode(v *unstable.Node, keyEnd, end int, src []byte) (*node, error) {
	start := skipSpace(src, keyEnd)
	if start >= end || src[start] != '=' {
		return nil, errors.New("expected '=' after key")
	}
	start = skipSpace(src, start+1)
	n := &node{val: Value{Span: [2]int{start, end}}}
	text := string(src[start:end])
	switch v.Kind {
	case unstable.String:
		n.val.Kind, n.val.Str = String, string(v.Data)
	case unstable.Integer:
		n.val.Kind, n.val.Str = Integer, text
		n.val.Int, _ = strconv.ParseInt(strings.ReplaceAll(text, "_", ""), 0, 64)
	case unstable.Float:
		n.val.Kind, n.val.Str = Float, text
	case unstable.Bool:
		n.val.Kind, n.val.Bool = Bool, text == "true"
	case unstable.Array:
		n.val.Kind = Array
	case unstable.InlineTable:
		n.val.Kind, n.kids = InlineTable, map[string]*node{}
		n.lastEnd = start + 1
		it := v.Children()
		for it.Next() {
			if _, err := addPair(n, it.Node(), src); err != nil {
				return nil, err
			}
			n.lastEnd = rawEnd(it.Node())
		}
	default:
		n.val.Kind, n.val.Str = Datetime, text
	}
	return n, nil
}

func skipSpace(src []byte, i int) int {
	for i < len(src) && (src[i] == ' ' || src[i] == '\t') {
		i++
	}
	return i
}
