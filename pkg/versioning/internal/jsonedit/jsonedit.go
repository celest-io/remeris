// Package jsonedit is a small ordered JSON tree built on encoding/json/jsontext.
//
// It reproduces what Knope does with serde_json (feature preserve_order, no arbitrary_precision): objects keep
// their member order, and Marshal prints like serde_json::to_string_pretty.
//
// Output rules:
//   - two-space indent, `"key": value`, `{}` and `[]` for empty containers, no trailing newline;
//   - strings escape only `"`, `\`, \b, \f, \n, \r, \t and the other control characters below U+0020 as
//     \u00xx (lowercase hex); `/`, `<`, `>`, `&`, DEL and non-ASCII are written as they are;
//   - numbers without a fraction or exponent that fit in uint64 (or int64 when negative) stay integers;
//     everything else goes through float64 and prints in Rust's shortest form (ryu): 1e3 becomes 1000.0,
//     1.0 stays 1.0, 1e16 becomes 1e16, 1e-7 becomes 1e-7, and -0 becomes -0.0;
//   - duplicate member names are accepted: the last value wins at the position of the first occurrence.
package jsonedit

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"math"
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
	maxDepth  = 128 // serde_json's recursion limit
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

// String returns a string value.
func String(s string) *Value { return &Value{kind: KindString, text: s} }

// NewObject returns an object with the given members, in order. A repeated name keeps the last value
// at the first position.
func NewObject(members ...Member) *Value {
	o := &Value{kind: KindObject}
	for _, m := range members {
		o.Set(m.Name, m.Value)
	}
	return o
}

// NewArray returns an array of the given elements.
func NewArray(elements ...*Value) *Value {
	return &Value{kind: KindArray, elements: append([]*Value(nil), elements...)}
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

// Set replaces the member called name in place, or appends it at the end. It does nothing on a non-object.
func (v *Value) Set(name string, val *Value) {
	if v.kind != KindObject {
		return
	}
	if i := v.index(name); i >= 0 {
		v.members[i].Value = val
		return
	}
	v.members = append(v.members, Member{Name: name, Value: val})
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

// Members returns the members of an object in order. The slice is a copy.
func (v *Value) Members() []Member { return append([]Member(nil), v.members...) }

// Elements returns the elements of an array. The slice is a copy.
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
	if depth > maxDepth {
		return nil, errors.New("recursion limit exceeded")
	}
	tok, err := dec.ReadToken()
	if err != nil {
		return nil, err
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

// numberText converts JSON number text to the form serde_json prints.
func numberText(raw string) (string, error) {
	if !strings.ContainsAny(raw, ".eE") {
		if raw == "-0" {
			return "-0.0", nil // serde_json parses "-0" as the float -0.0
		}
		if raw[0] == '-' {
			if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
				return strconv.FormatInt(n, 10), nil
			}
		} else if n, err := strconv.ParseUint(raw, 10, 64); err == nil {
			return strconv.FormatUint(n, 10), nil
		}
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsInf(f, 0) {
		return "", fmt.Errorf("number out of range: %s", raw)
	}
	return formatFloat(f), nil
}

// formatFloat prints f like Rust's ryu (the serde_json float formatter).
func formatFloat(f float64) string {
	sign := ""
	if math.Signbit(f) {
		sign = "-"
		f = -f
	}
	if f == 0 {
		return sign + "0.0"
	}
	// 'e' with -1 precision yields the shortest digits: d.ddde±XX
	s := strconv.FormatFloat(f, 'e', -1, 64)
	mant, expText, _ := strings.Cut(s, "e")
	exp, _ := strconv.Atoi(expText)
	digits := strings.Replace(mant, ".", "", 1)
	n := len(digits)
	exp10 := exp - (n - 1) // value = digits * 10^exp10
	kk := n + exp10
	switch {
	case exp10 >= 0 && kk <= 16:
		return sign + digits + strings.Repeat("0", exp10) + ".0"
	case kk > 0 && kk <= 16:
		return sign + digits[:kk] + "." + digits[kk:]
	case kk > -5 && kk <= 0:
		return sign + "0." + strings.Repeat("0", -kk) + digits
	case n == 1:
		return sign + digits + "e" + strconv.Itoa(kk-1)
	default:
		return sign + digits[:1] + "." + digits[1:] + "e" + strconv.Itoa(kk-1)
	}
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
