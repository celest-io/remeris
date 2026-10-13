// Package jsonedit is a small ordered JSON tree built on encoding/json/jsontext.
//
// It reproduces what Knope does with serde_json 1.0.151 (features preserve_order; no arbitrary_precision, no
// float_roundtrip): objects keep their member order, and Marshal prints like serde_json::to_string_pretty.
// It targets package.json, package-lock.json and tauri.conf.json. Knope does not write deno.lock with
// serde_json (it uses deno_lockfile's canonical writer), so this package is not meant for that file.
//
// Parsing accepts at most 127 nested containers (arrays and objects; scalars do not count) and fails at the
// 128th, like serde_json's recursion limit.
//
// Output rules:
//   - two-space indent, `"key": value`, `{}` and `[]` for empty containers, no trailing newline;
//   - strings escape only `"`, `\`, \b, \f, \n, \r, \t and the other control characters below U+0020 as
//     \u00xx (lowercase hex); `/`, `<`, `>`, `&`, DEL and non-ASCII are written as they are;
//   - numbers without a fraction or exponent that fit in uint64 (or int64 when negative) stay integers;
//     everything else goes through float64, parsed the way serde_json does (not always correctly rounded: see
//     numberText) and printed like serde_json's zmij formatter, with an explicit sign on positive exponents:
//     1e3 becomes 1000.0, 1.0 stays 1.0, 1e16 becomes 1e+16, 1e-7 becomes 1e-7, and -0 becomes -0.0;
//   - duplicate member names are accepted: the last value wins at the position of the first occurrence.
package jsonedit

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Kind is the JSON type of a Value.
type Kind int

// The JSON kinds.
const (
	KindNull Kind = iota
	KindBool
	KindNumber
	KindString
	KindArray
	KindObject
)

const (
	indent    = "  "
	hexDigits = "0123456789abcdef"
	maxDepth  = 128 // serde_json's recursion limit: the 128th nested container is an error
)

// Member is one name/value pair of an object.
type Member struct {
	Name  string
	Value *Value
}

// Value is a node of the tree. The zero value is null.
type Value struct {
	kind     Kind
	text     string // Bool: "true"/"false"; Number: canonical output text; String: decoded text
	members  []Member
	elements []*Value
}

// Kind returns the JSON type of v.
func (v *Value) Kind() Kind { return v.kind }

// NewNull returns a null value.
func NewNull() *Value { return &Value{kind: KindNull} }

// NewBool returns a boolean value.
func NewBool(b bool) *Value { return &Value{kind: KindBool, text: strconv.FormatBool(b)} }

// NewInt returns an integer number.
func NewInt(n int64) *Value { return &Value{kind: KindNumber, text: strconv.FormatInt(n, 10)} }

// String returns a string value. A Rust String cannot hold invalid UTF-8, so each run of invalid bytes in s is
// replaced by one U+FFFD.
func String(s string) *Value { return &Value{kind: KindString, text: strings.ToValidUTF8(s, "�")} }

// orNull maps a nil value to JSON null, so a tree never holds a nil node.
func orNull(v *Value) *Value {
	if v == nil {
		return NewNull()
	}
	return v
}

// NewObject returns an object with the given members, in order. A repeated name keeps the last value
// at the first position. A nil value is stored as null.
func NewObject(members ...Member) *Value {
	o := &Value{kind: KindObject}
	for _, m := range members {
		o.Set(m.Name, m.Value)
	}
	return o
}

// NewArray returns an array of the given elements. A nil element is stored as null.
func NewArray(elements ...*Value) *Value {
	a := &Value{kind: KindArray, elements: make([]*Value, len(elements))}
	for i, el := range elements {
		a.elements[i] = orNull(el)
	}
	return a
}

// Str returns the decoded text of a string value.
func (v *Value) Str() (string, bool) {
	if v.kind != KindString {
		return "", false
	}
	return v.text, true
}

// Bool returns the value of a boolean.
func (v *Value) Bool() (value, ok bool) {
	if v.kind != KindBool {
		return false, false
	}
	return v.text == "true", true
}

// NumberText returns the number as Marshal prints it (the serde_json form).
func (v *Value) NumberText() (string, bool) {
	if v.kind != KindNumber {
		return "", false
	}
	return v.text, true
}

// Get returns the member called name of an object.
func (v *Value) Get(name string) (*Value, bool) {
	if i := v.index(name); i >= 0 {
		return v.members[i].Value, true
	}
	return nil, false
}

func (v *Value) index(name string) int {
	for i, m := range v.members {
		if m.Name == name {
			return i
		}
	}
	return -1
}

// Set replaces the member called name in place, or appends it at the end. A nil val is stored as null. It reports
// false, and changes nothing, when v is not an object.
func (v *Value) Set(name string, val *Value) bool {
	if v.kind != KindObject {
		return false
	}
	val = orNull(val)
	if i := v.index(name); i >= 0 {
		v.members[i].Value = val
		return true
	}
	v.members = append(v.members, Member{Name: name, Value: val})
	return true
}

// Rename changes the name of a member without moving it. It reports false when oldName is absent or newName is
// already used by another member.
func (v *Value) Rename(oldName, newName string) bool {
	i := v.index(oldName)
	if i < 0 {
		return false
	}
	if oldName == newName {
		return true
	}
	if v.index(newName) >= 0 {
		return false
	}
	v.members[i].Name = newName
	return true
}

// Members returns the members of an object in order. The slice is a copy, but the values are shared with v.
func (v *Value) Members() []Member { return append([]Member(nil), v.members...) }

// Elements returns the elements of an array. The slice is a copy, but the values are shared with v.
func (v *Value) Elements() []*Value { return append([]*Value(nil), v.elements...) }

// Parse reads exactly one JSON value from src.
func Parse(src []byte) (*Value, error) {
	dec := jsontext.NewDecoder(bytes.NewReader(src), jsontext.AllowDuplicateNames(true))
	v, err := parseValue(dec, 0)
	if err != nil {
		return nil, err
	}
	if _, err := dec.ReadToken(); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("trailing characters after the top-level value")
		}
		return nil, err
	}
	return v, nil
}

func parseValue(dec *jsontext.Decoder, depth int) (*Value, error) {
	tok, err := dec.ReadToken()
	if err != nil {
		return nil, err
	}
	if k := tok.Kind(); (k == '[' || k == '{') && depth+1 >= maxDepth {
		return nil, errors.New("recursion limit exceeded")
	}
	switch tok.Kind() {
	case 'n':
		return NewNull(), nil
	case 't', 'f':
		return NewBool(tok.Bool()), nil
	case '"':
		return String(tok.String()), nil
	case '0':
		text, err := numberText(tok.String())
		if err != nil {
			return nil, err
		}
		return &Value{kind: KindNumber, text: text}, nil
	case '[':
		arr := &Value{kind: KindArray}
		for dec.PeekKind() != ']' {
			el, err := parseValue(dec, depth+1)
			if err != nil {
				return nil, err
			}
			arr.elements = append(arr.elements, el)
		}
		_, err := dec.ReadToken() // ]
		return arr, err
	case '{':
		obj := &Value{kind: KindObject}
		for dec.PeekKind() != '}' {
			nameTok, err := dec.ReadToken()
			if err != nil {
				return nil, err
			}
			name := nameTok.String() // a token is only valid until the next read
			val, err := parseValue(dec, depth+1)
			if err != nil {
				return nil, err
			}
			obj.Set(name, val)
		}
		_, err := dec.ReadToken() // }
		return obj, err
	}
	return nil, fmt.Errorf("unexpected token %q", tok.String())
}

// Marshal prints v like serde_json::to_string_pretty.
func Marshal(v *Value) []byte {
	var b bytes.Buffer
	write(&b, v, 0)
	return b.Bytes()
}

func write(b *bytes.Buffer, v *Value, level int) {
	switch v.kind {
	case KindNull:
		b.WriteString("null")
	case KindBool, KindNumber:
		b.WriteString(v.text)
	case KindString:
		writeString(b, v.text)
	case KindArray:
		if len(v.elements) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		for i, el := range v.elements {
			if i > 0 {
				b.WriteString(",\n")
			}
			b.WriteString(strings.Repeat(indent, level+1))
			write(b, el, level+1)
		}
		b.WriteString("\n" + strings.Repeat(indent, level) + "]")
	case KindObject:
		if len(v.members) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString("{\n")
		for i, m := range v.members {
			if i > 0 {
				b.WriteString(",\n")
			}
			b.WriteString(strings.Repeat(indent, level+1))
			writeString(b, m.Name)
			b.WriteString(": ")
			write(b, m.Value, level+1)
		}
		b.WriteString("\n" + strings.Repeat(indent, level) + "}")
	}
}

func writeString(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for i := range len(s) {
		c := s[i]
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 {
				b.WriteString(`\u00`)
				b.WriteByte(hexDigits[c>>4])
				b.WriteByte(hexDigits[c&0xf])
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
}
