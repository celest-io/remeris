package tomlwrite_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2/unstable"

	"github.com/celest-io/remeris/internal/tomlwrite"
)

// The order of the keys is part of the output (Knope's goldens depend on it), and a map comparison cannot see it.
// These helpers list the keys of a document, each as its full path, in the order a TOML parser meets them, and the
// order the layout promises.

const arrayMark = "[]"

func joinPath(parts []string) string { return strings.Join(parts, "\x00") }

func extend(path []string, parts ...string) []string {
	return append(append([]string(nil), path...), parts...)
}

// modelEvents is the order the layout promises: in each table the keys that are written on one line, in the order
// they were set, then the sections.
func modelEvents(entries []tomlwrite.Entry, prefix []string, out *[]string) {
	var sections []tomlwrite.Entry
	for _, e := range entries {
		switch v := e.Value.(type) {
		case *tomlwrite.Table:
			sections = append(sections, e)
			continue
		case *tomlwrite.Array:
			if isArrayOfTables(v) {
				sections = append(sections, e)
				continue
			}
		}
		path := extend(prefix, e.Key)
		*out = append(*out, joinPath(path))
		inlineEvents(e.Value, path, out)
	}
	for _, e := range sections {
		path := extend(prefix, e.Key)
		switch v := e.Value.(type) {
		case *tomlwrite.Table:
			modelEvents(v.Entries(), path, out)
		case *tomlwrite.Array:
			for _, item := range v.Items() {
				if element, ok := item.(*tomlwrite.Table); ok {
					modelEvents(element.Entries(), path, out)
				}
			}
		}
	}
}

func isArrayOfTables(a *tomlwrite.Array) bool {
	if a.Len() == 0 {
		return false
	}
	for _, item := range a.Items() {
		if _, ok := item.(*tomlwrite.Table); !ok {
			return false
		}
	}
	return true
}

func inlineEvents(v tomlwrite.Value, path []string, out *[]string) {
	switch v := v.(type) {
	case *tomlwrite.Table:
		inlineEntries(v.Entries(), path, out)
	case *tomlwrite.InlineTable:
		inlineEntries(v.Entries(), path, out)
	case *tomlwrite.Array:
		for _, item := range v.Items() {
			inlineEvents(item, extend(path, arrayMark), out)
		}
	}
}

func inlineEntries(entries []tomlwrite.Entry, path []string, out *[]string) {
	for _, e := range entries {
		p := extend(path, e.Key)
		*out = append(*out, joinPath(p))
		inlineEvents(e.Value, p, out)
	}
}

func keyParts(n *unstable.Node) []string {
	var parts []string
	it := n.Key()
	for it.Next() {
		parts = append(parts, string(it.Node().Data))
	}
	return parts
}

func parsedInline(n *unstable.Node, path []string, out *[]string) {
	switch n.Kind {
	case unstable.InlineTable:
		it := n.Children()
		for it.Next() {
			kv := it.Node()
			p := extend(path, keyParts(kv)...)
			*out = append(*out, joinPath(p))
			parsedInline(kv.Value(), p, out)
		}
	case unstable.Array:
		it := n.Children()
		for it.Next() {
			parsedInline(it.Node(), extend(path, arrayMark), out)
		}
	}
}

func parsedEvents(data []byte) ([]string, error) {
	var p unstable.Parser
	p.Reset(data)
	var out, header []string
	for p.NextExpression() {
		n := p.Expression()
		switch n.Kind {
		case unstable.Table, unstable.ArrayTable:
			header = keyParts(n)
		case unstable.KeyValue:
			path := extend(header, keyParts(n)...)
			out = append(out, joinPath(path))
			parsedInline(n.Value(), path, &out)
		}
	}
	return out, p.Error()
}

func checkKeyOrder(t *testing.T, d *tomlwrite.Doc, text string) {
	t.Helper()
	got, err := parsedEvents([]byte(text))
	if err != nil {
		t.Fatalf("unstable parser: %v\n%s", err, text)
	}
	var want []string
	modelEvents(d.Entries(), nil, &want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("key order differs\n got: %q\nwant: %q\ntext:\n%s", got, want, text)
	}
}
