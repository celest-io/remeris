// Package tomlwrite writes TOML the way Knope's Rust `toml::to_string` does (toml 1.1.8 over toml_writer 1.1.3), for
// the small schema of knope.toml. `--generate` and `--upgrade` use it (spec F20.7), and they need the same bytes as
// Knope: quoting, inline tables, blank lines and key order.
//
// It is not a marshaller. The caller builds a document from a few value kinds, in the order the keys must appear,
// and [Doc.Encode] prints it. Nothing is read through reflection and no map is iterated, so the output is
// deterministic. Package names and other text come from files the user does not control, so every string and key
// goes through the encoders in encode.go, which quote and escape as toml_writer does; the text of the document is
// never built any other way, and a hostile string can only ever be data.
//
// Every string, key and package name put in a document comes from a manifest or a file the user does not control.
// Always build documents with the types of this package and never concatenate TOML text yourself, not even a header
// or a key: the encoders are the only code that knows how to quote.
//
// The layout follows the document serialiser of the toml crate:
//
//   - the scalars, arrays and inline tables of a table come first, then its sub-tables, whatever the order they
//     were set in;
//   - a [Table] under a table is a `[a.b]` section, and an [Array] whose elements are all [Table]s is a `[[a.b]]`
//     section per element; every other array is written inline, `[1, 2]`, and an [InlineTable] is written inline,
//     `{ a = "b", c = "d" }`, where it sits;
//   - a table that has no keys of its own but has sections below it gets no header of its own (`[packages]` is
//     implied by `[packages.first]`), an empty table does get one, and an element of an array of tables always does;
//   - a blank line comes before every header except at the very start, and the output ends with one line feed.
//
// The toml crate treats a struct as a table and a map as a table too, so it never prints an inline table where a
// section could stand. [InlineTable] is the way to ask for one anyway; Knope's schema does not need it for
// anything but the entries of an array that also holds strings.
package tomlwrite

import (
	"errors"
	"io"
	"strconv"
	"strings"
)

// maxDepth is how deep tables, inline tables and arrays may nest. A real knope.toml is a handful of levels deep;
// the limit turns a document that contains itself into an error instead of a stack overflow.
const maxDepth = 256

// ErrTooDeep is returned when a document nests more than 256 levels, which includes a document that contains itself.
var ErrTooDeep = errors.New("tomlwrite: document is nested too deeply")

// errUnknownValue is returned for a Value this package does not know. The interface is sealed, so it should not
// happen; it is an error, never empty text, so that no invalid TOML can be written.
var errUnknownValue = errors.New("tomlwrite: unknown value type")

// Value is a TOML value: [String], [Bool], [Int], *[Array], *[Table] or *[InlineTable].
type Value interface {
	isValue()
}

// String is a TOML string.
type String string

// Bool is a TOML boolean.
type Bool bool

// Int is a TOML integer.
type Int int64

func (String) isValue()       {}
func (Bool) isValue()         {}
func (Int) isValue()          {}
func (*Array) isValue()       {}
func (*Table) isValue()       {}
func (*InlineTable) isValue() {}

// Entry is a key and its value.
type Entry struct {
	Key   string
	Value Value
}

// isNilValue reports whether v is nil or a typed nil pointer, which is the same as no value.
func isNilValue(v Value) bool {
	switch v := v.(type) {
	case nil:
		return true
	case *Array:
		return v == nil
	case *Table:
		return v == nil
	case *InlineTable:
		return v == nil
	}
	return false
}

// entries is an ordered set of keys. A key is stored once; setting it again keeps its place.
type entries struct {
	list []Entry
}

// Set stores v under key. An existing key keeps its position and gets the new value. A nil v removes the key,
// as Rust's toml leaves out a `None`.
func (e *entries) Set(key string, v Value) {
	if isNilValue(v) {
		v = nil
	}
	for i := range e.list {
		if e.list[i].Key != key {
			continue
		}
		if v == nil {
			e.list = append(e.list[:i], e.list[i+1:]...)
		} else {
			e.list[i].Value = v
		}
		return
	}
	if v != nil {
		e.list = append(e.list, Entry{Key: key, Value: v})
	}
}

// Get returns the value stored under key.
func (e *entries) Get(key string) (Value, bool) {
	for _, entry := range e.list {
		if entry.Key == key {
			return entry.Value, true
		}
	}
	return nil, false
}

// Len returns the number of keys.
func (e *entries) Len() int { return len(e.list) }

// Entries returns a copy of the keys and values, in order.
func (e *entries) Entries() []Entry { return append([]Entry(nil), e.list...) }

// Table is an ordered table of keys. Under another table, or under an element of an array of tables, it is
// written as a `[section]`; anywhere else (in an array that also holds other kinds, or in an inline table) it is
// written inline.
type Table struct {
	entries
}

// NewTable returns an empty table.
func NewTable() *Table { return &Table{} }

// InlineTable is an ordered table that is always written inline, `{ a = "b" }`.
type InlineTable struct {
	entries
}

// NewInlineTable returns an empty inline table.
func NewInlineTable() *InlineTable { return &InlineTable{} }

// Array is an array. It is written inline unless it is not empty and all its elements are *[Table]: then each
// element is a `[[section]]`.
type Array struct {
	items []Value
}

// NewArray returns an array holding items. Nil items are left out.
func NewArray(items ...Value) *Array {
	a := &Array{}
	a.Append(items...)
	return a
}

// NewArrayOfTables returns an array of the tables, which the document writes as `[[section]]`s.
func NewArrayOfTables(tables ...*Table) *Array {
	a := &Array{}
	for _, t := range tables {
		if t != nil { // a typed nil would otherwise sit in the array
			a.items = append(a.items, t)
		}
	}
	return a
}

// Append adds items at the end. Nil items are left out.
func (a *Array) Append(items ...Value) {
	for _, item := range items {
		if !isNilValue(item) {
			a.items = append(a.items, item)
		}
	}
}

// Len returns the number of elements.
func (a *Array) Len() int { return len(a.items) }

// Items returns a copy of the elements.
func (a *Array) Items() []Value { return append([]Value(nil), a.items...) }

// Doc is a TOML document. It is not a [Value]: it cannot be put inside another document.
type Doc struct {
	root Table
}

// NewDoc returns an empty document.
func NewDoc() *Doc { return &Doc{} }

// Set stores v under key at the top level, as [Table.Set] does.
func (d *Doc) Set(key string, v Value) { d.root.Set(key, v) }

// Get returns the value stored under key at the top level.
func (d *Doc) Get(key string) (Value, bool) { return d.root.Get(key) }

// Len returns the number of top-level keys.
func (d *Doc) Len() int { return d.root.Len() }

// Entries returns a copy of the top-level keys and values, in order.
func (d *Doc) Entries() []Entry { return d.root.Entries() }

// Encode returns the document as TOML. The error is [ErrInvalidUTF8] when a string or a key is not valid UTF-8 and
// [ErrTooDeep] when it nests more than 256 levels; the bytes are then nil.
//
// An empty document encodes to no bytes and a nil error. A caller that replaces a file (`--generate`, `--upgrade`)
// must refuse to replace a non-empty file with empty output, and must check the error before it writes anything.
func (d *Doc) Encode() ([]byte, error) {
	w := &writer{}
	if err := w.table(&d.root, nil, false, 0); err != nil {
		return nil, err
	}
	return []byte(w.String()), nil
}

// WriteTo writes the document to out. It writes nothing when the document cannot be encoded.
func (d *Doc) WriteTo(out io.Writer) (int64, error) {
	b, err := d.Encode()
	if err != nil {
		return 0, err
	}
	n, err := out.Write(b)
	return int64(n), err
}

// section is one `[header]` and its body, the Table of the toml crate's document buffer.
type section struct {
	path        []string // encoded keys; nil for the root
	body        strings.Builder
	hasChildren bool
	array       bool
}

// required reports whether the section is printed (the buffer's required_table).
func (s *section) required() bool {
	if s.path == nil {
		return s.body.Len() > 0
	}
	return s.array || s.body.Len() > 0 || !s.hasChildren
}

func (s *section) String() string {
	var sb strings.Builder
	if s.path != nil {
		if s.array {
			sb.WriteString("[[")
		} else {
			sb.WriteByte('[')
		}
		sb.WriteString(strings.Join(s.path, "."))
		if s.array {
			sb.WriteString("]]")
		} else {
			sb.WriteByte(']')
		}
		sb.WriteByte('\n')
	}
	sb.WriteString(s.body.String())
	return sb.String()
}

// writer collects the sections in the order the toml crate creates them: a parent, then its children.
type writer struct {
	sections []*section
}

func (w *writer) String() string {
	var sb strings.Builder
	first := true
	for _, s := range w.sections {
		if !s.required() {
			continue
		}
		if !first {
			sb.WriteByte('\n')
		}
		first = false
		sb.WriteString(s.String())
	}
	return sb.String()
}

// table adds the section for t, found at path (encoded keys), and the sections below it.
func (w *writer) table(t *Table, path []string, array bool, depth int) error {
	if depth > maxDepth {
		return ErrTooDeep
	}
	sec := &section{path: path, array: array}
	w.sections = append(w.sections, sec) // reserves the position before the children
	for _, entry := range t.list {
		key, err := encodeKey(entry.Key)
		if err != nil {
			return err
		}
		child := append(append([]string(nil), path...), key)
		switch v := entry.Value.(type) {
		case *Table:
			sec.hasChildren = true
			if err := w.table(v, child, false, depth+1); err != nil {
				return err
			}
		case *Array:
			if !v.isArrayOfTables() {
				if err := writeKeyValue(&sec.body, key, v, depth+1); err != nil {
					return err
				}
				continue
			}
			sec.hasChildren = true
			for _, item := range v.items {
				if err := w.table(item.(*Table), child, true, depth+1); err != nil {
					return err
				}
			}
		case String, Bool, Int, *InlineTable:
			if err := writeKeyValue(&sec.body, key, v, depth+1); err != nil {
				return err
			}
		default:
			return errUnknownValue
		}
	}
	return nil
}

// isArrayOfTables reports whether the array is written as `[[sections]]`.
func (a *Array) isArrayOfTables() bool {
	if len(a.items) == 0 {
		return false
	}
	for _, item := range a.items {
		if _, ok := item.(*Table); !ok {
			return false
		}
	}
	return true
}

// writeKeyValue writes `key = value` and a line feed. The key is already encoded.
func writeKeyValue(sb *strings.Builder, key string, v Value, depth int) error {
	sb.WriteString(key)
	sb.WriteString(" = ")
	if err := writeInline(sb, v, depth); err != nil {
		return err
	}
	sb.WriteByte('\n')
	return nil
}

// writeInline writes v on one line.
func writeInline(sb *strings.Builder, v Value, depth int) error {
	if depth > maxDepth {
		return ErrTooDeep
	}
	switch v := v.(type) {
	case String:
		s, err := encodeString(string(v))
		if err != nil {
			return err
		}
		sb.WriteString(s)
	case Bool:
		sb.WriteString(strconv.FormatBool(bool(v)))
	case Int:
		sb.WriteString(strconv.FormatInt(int64(v), 10))
	case *Array:
		sb.WriteByte('[')
		for i, item := range v.items {
			if i > 0 {
				sb.WriteString(", ")
			}
			if err := writeInline(sb, item, depth+1); err != nil {
				return err
			}
		}
		sb.WriteByte(']')
	case *Table:
		return writeInlineTable(sb, v.list, depth)
	case *InlineTable:
		return writeInlineTable(sb, v.list, depth)
	default:
		return errUnknownValue
	}
	return nil
}

// writeInlineTable writes `{ a = "b", c = "d" }`, or `{}` when there are no keys.
func writeInlineTable(sb *strings.Builder, list []Entry, depth int) error {
	sb.WriteByte('{')
	for i, entry := range list {
		if i > 0 {
			sb.WriteByte(',')
		}
		key, err := encodeKey(entry.Key)
		if err != nil {
			return err
		}
		sb.WriteByte(' ')
		sb.WriteString(key)
		sb.WriteString(" = ")
		if err := writeInline(sb, entry.Value, depth+1); err != nil {
			return err
		}
	}
	if len(list) > 0 {
		sb.WriteByte(' ')
	}
	sb.WriteByte('}')
	return nil
}
