package config

import (
	"github.com/pelletier/go-toml/v2/unstable"
)

// Span is a byte range [Start, End) of the TOML source.
type Span struct{ Start, End int }

// tomlIndex records what koanf's decoded maps lose: the order in which keys were declared and
// where tables and array elements sit in the source (spec F2.4).
//
// It is a tree that mirrors the document. Look nodes up with at, passing a string for a table key
// or an int for an array element: at("workflows", 0, "steps", 1, "variables").
type tomlIndex struct {
	root *indexNode
}

type indexNode struct {
	span  Span
	keys  []string              // table: keys in declaration order
	kids  map[string]*indexNode // table: values by key
	elems []*indexNode          // array: elements in order
}

func newTable(span Span) *indexNode {
	return &indexNode{span: span, kids: map[string]*indexNode{}}
}

// child returns the table value under key, creating it with makeNode on first use.
func (n *indexNode) child(key string, makeNode func() *indexNode) *indexNode {
	if kid, ok := n.kids[key]; ok {
		return kid
	}
	kid := makeNode()
	n.kids[key] = kid
	n.keys = append(n.keys, key)
	return kid
}

// at follows path from n. It returns nil if any step is missing or has the wrong shape.
func (n *indexNode) at(path ...any) *indexNode {
	for _, p := range path {
		if n == nil {
			return nil
		}
		switch p := p.(type) {
		case string:
			n = n.kids[p]
		case int:
			if p < 0 || p >= len(n.elems) {
				return nil
			}
			n = n.elems[p]
		default:
			return nil
		}
	}
	return n
}

// Keys returns the keys of the table at path in declaration order, or nil if there is no such table.
func (i *tomlIndex) Keys(path ...any) []string {
	if n := i.root.at(path...); n != nil {
		return n.keys
	}
	return nil
}

// Span returns the source range of the table or array element at path.
func (i *tomlIndex) Span(path ...any) (Span, bool) {
	n := i.root.at(path...)
	if n == nil {
		return Span{}, false
	}
	return n.span, true
}

// buildIndex parses src and indexes it. Errors are not expected for input koanf already parsed,
// but a parse error is returned rather than hidden.
func buildIndex(src []byte) (*tomlIndex, error) {
	p := unstable.Parser{}
	p.Reset(src)

	root := newTable(Span{0, len(src)})
	cur := root
	var open *indexNode // the table or array element whose header was seen last; its span ends at the next header

	closeOpen := func(at int) {
		if open != nil {
			open.span.End = at
		}
	}

	for p.NextExpression() {
		expr := p.Expression()
		switch expr.Kind {
		case unstable.Table, unstable.ArrayTable:
			start := headerStart(&p, expr)
			closeOpen(start)
			keys := keyParts(expr.Key())
			cur = root
			for _, k := range keys[:len(keys)-1] {
				cur = descend(cur, k)
			}
			last := keys[len(keys)-1]
			if expr.Kind == unstable.Table {
				cur = cur.child(last, func() *indexNode { return newTable(Span{start, len(src)}) })
				if cur.span.Start == 0 && start != 0 {
					cur.span.Start = start
				}
			} else {
				arr := cur.child(last, func() *indexNode { return &indexNode{} })
				cur = newTable(Span{start, len(src)})
				arr.elems = append(arr.elems, cur)
			}
			open = cur
		case unstable.KeyValue:
			keys := keyParts(expr.Key())
			tbl := cur
			for _, k := range keys[:len(keys)-1] {
				tbl = descend(tbl, k)
			}
			last := keys[len(keys)-1]
			tbl.child(last, func() *indexNode { return valueNode(&p, expr.Value()) })
		}
	}
	if err := p.Error(); err != nil {
		return nil, err
	}
	return &tomlIndex{root: root}, nil
}

// descend returns the table under key in n for a dotted key or a dotted header. If key is an
// array of tables, it continues into the array's last element, as TOML specifies.
func descend(n *indexNode, key string) *indexNode {
	kid := n.child(key, func() *indexNode { return newTable(Span{}) })
	if len(kid.elems) > 0 {
		return kid.elems[len(kid.elems)-1]
	}
	return kid
}

// valueNode indexes a value. Inline tables and arrays get children, scalars are leaves.
func valueNode(p *unstable.Parser, v *unstable.Node) *indexNode {
	span, _ := rangeOf(p, v)
	switch v.Kind {
	case unstable.InlineTable:
		n := newTable(span)
		it := v.Children()
		for it.Next() {
			kv := it.Node()
			keys := keyParts(kv.Key())
			tbl := n
			for _, k := range keys[:len(keys)-1] {
				tbl = descend(tbl, k)
			}
			tbl.child(keys[len(keys)-1], func() *indexNode { return valueNode(p, kv.Value()) })
		}
		return n
	case unstable.Array:
		n := &indexNode{span: span}
		it := v.Children()
		for it.Next() {
			n.elems = append(n.elems, valueNode(p, it.Node()))
		}
		return n
	}
	return &indexNode{span: span}
}

func keyParts(it unstable.Iterator) []string {
	var keys []string
	for it.Next() {
		keys = append(keys, string(it.Node().Data))
	}
	return keys
}

// rangeOf returns the source range of a node, if the parser can tell. Strings with escape
// sequences carry freshly allocated data and have no range.
func rangeOf(p *unstable.Parser, n *unstable.Node) (span Span, ok bool) {
	defer func() {
		if recover() != nil {
			span, ok = Span{}, false
		}
	}()
	if n.Raw.Length > 0 {
		return Span{int(n.Raw.Offset), int(n.Raw.Offset + n.Raw.Length)}, true
	}
	r := p.Range(n.Data)
	return Span{int(r.Offset), int(r.Offset + r.Length)}, true
}

// headerStart is the offset of the `[` that opens a table header. The parser reports the range of
// the key, so walk back over the brackets and whitespace.
func headerStart(p *unstable.Parser, expr *unstable.Node) int {
	it := expr.Key()
	if !it.Next() {
		return 0
	}
	span, ok := rangeOf(p, it.Node())
	if !ok {
		return 0
	}
	data := p.Data()
	i := span.Start
	for i > 0 && (data[i-1] == '[' || data[i-1] == ' ' || data[i-1] == '\t' || data[i-1] == '"' || data[i-1] == '\'') {
		i--
	}
	return i
}
