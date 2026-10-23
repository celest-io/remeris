package denolock

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
type Kind uint8

// The JSON kinds.
const (
	KindNull Kind = iota
	KindBool
	KindNumber
	KindString
	KindArray
	KindObject
)

// maxDepth is serde_json's recursion limit: the 128th nested container is an error.
const maxDepth = 128

// objectIndexThreshold is the object size from which parsing keeps a hash index to find repeated names, so that a
// large object (a lockfile "npm" section) is read in linear time.
const objectIndexThreshold = 16

// Value is a node of an ordered JSON tree, the equivalent of serde_json::Value with the preserve_order feature (the
// way Knope builds it). The zero value is null.
//
// Str holds the decoded text of a string; for a number it holds the text serde_json prints (an integer stays an
// integer, anything else goes through float64: see numberText); for a boolean it is "true" or "false".
type Value struct {
	Kind Kind
	Str  string
	Arr  []Value
	Obj  []Member
}

// Member is one name and value of an object.
type Member struct {
	Key   string
	Value Value
}

// String returns a string value.
func String(s string) Value { return Value{Kind: KindString, Str: s} }

// Bool returns a boolean value.
func Bool(b bool) Value { return Value{Kind: KindBool, Str: strconv.FormatBool(b)} }

// Get returns the member called key of an object. It is a linear search: use it on the small objects of the
// lockfile, not to index a large section.
func (v Value) Get(key string) (Value, bool) {
	if v.Kind != KindObject {
		return Value{}, false
	}
	for _, m := range v.Obj {
		if m.Key == key {
			return m.Value, true
		}
	}
	return Value{}, false
}

// StringValue returns the text of a string value.
func (v Value) StringValue() (string, bool) {
	if v.Kind != KindString {
		return "", false
	}
	return v.Str, true
}

// ParseJSON reads exactly one JSON value, the way Knope reads a lockfile with serde_json::from_str into a Value:
// objects keep their member order, a repeated name keeps the last value at the position of the first occurrence,
// and at most 127 containers may be nested (the 128th fails with "recursion limit exceeded"). The error texts
// are those of Go's JSON reader, not serde_json's.
func ParseJSON(src []byte) (Value, error) {
	dec := jsontext.NewDecoder(bytes.NewReader(src), jsontext.AllowDuplicateNames(true))
	v, err := parseValue(dec, 0)
	if err != nil {
		return Value{}, err
	}
	if _, err := dec.ReadToken(); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("trailing characters after the top-level value")
		}
		return Value{}, err
	}
	return v, nil
}

func parseValue(dec *jsontext.Decoder, depth int) (Value, error) {
	tok, err := dec.ReadToken()
	if err != nil {
		return Value{}, err
	}
	if k := tok.Kind(); (k == '[' || k == '{') && depth+1 >= maxDepth {
		return Value{}, errors.New("recursion limit exceeded")
	}
	switch tok.Kind() {
	case 'n':
		return Value{}, nil
	case 't', 'f':
		return Bool(tok.Bool()), nil
	case '"':
		return String(tok.String()), nil
	case '0':
		text, err := numberText(tok.String())
		if err != nil {
			return Value{}, err
		}
		return Value{Kind: KindNumber, Str: text}, nil
	case '[':
		return parseArray(dec, depth)
	case '{':
		return parseObject(dec, depth)
	}
	return Value{}, fmt.Errorf("unexpected token %q", tok.String())
}

func parseArray(dec *jsontext.Decoder, depth int) (Value, error) {
	arr := Value{Kind: KindArray}
	for dec.PeekKind() != ']' {
		el, err := parseValue(dec, depth+1)
		if err != nil {
			return Value{}, err
		}
		arr.Arr = append(arr.Arr, el)
	}
	_, err := dec.ReadToken() // ]
	return arr, err
}

func parseObject(dec *jsontext.Decoder, depth int) (Value, error) {
	obj := Value{Kind: KindObject}
	var index map[string]int
	for dec.PeekKind() != '}' {
		nameTok, err := dec.ReadToken()
		if err != nil {
			return Value{}, err
		}
		name := nameTok.String() // a token is only valid until the next read
		val, err := parseValue(dec, depth+1)
		if err != nil {
			return Value{}, err
		}
		i := -1
		switch {
		case index != nil:
			if j, ok := index[name]; ok {
				i = j
			}
		default:
			for j := range obj.Obj {
				if obj.Obj[j].Key == name {
					i = j
					break
				}
			}
		}
		if i >= 0 {
			obj.Obj[i].Value = val
			continue
		}
		obj.Obj = append(obj.Obj, Member{Key: name, Value: val})
		switch {
		case index != nil:
			index[name] = len(obj.Obj) - 1
		case len(obj.Obj) > objectIndexThreshold:
			index = make(map[string]int, 2*len(obj.Obj))
			for j := range obj.Obj {
				index[obj.Obj[j].Key] = j
			}
		}
	}
	_, err := dec.ReadToken() // }
	return obj, err
}

// isFloatText reports whether the canonical number text is a float (an integer prints without '.' or 'e').
func isFloatText(text string) bool {
	return strings.ContainsAny(text, ".eE")
}
