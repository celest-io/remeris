package tomledit

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Errors returned by the editing functions (ErrSyntax is declared in tomledit.go). Use errors.Is to test for them.
var (
	// ErrNotValue means the key exists but is a table or an array of tables, whose bytes cannot be replaced.
	ErrNotValue = errors.New("key is a table, not a value")
	// ErrNoParent means the table that should receive a new key does not exist.
	ErrNoParent = errors.New("parent table not found")
	// ErrNotInsertable means a key cannot be inserted into the parent: only the root, [table] headers,
	// array-of-tables elements, tables that dotted keys define and inline tables accept new keys.
	ErrNotInsertable = errors.New("cannot insert a key into this kind of table")
	// ErrNotString means the value to edit is not a string.
	ErrNotString = errors.New("value is not a string")
	// ErrUnrepresentable means a string cannot be written in the quoting style of the value it replaces.
	ErrUnrepresentable = errors.New("string cannot be represented in this quoting style")
)

// SetString sets the key at path to the basic string v, escaped as TOML requires.
//
// If the key exists and is not a table, the bytes of its value are replaced and nothing around them changes (a
// trailing comment stays). If it does not exist, it is inserted the way toml_edit does it:
//   - in the root table or a [table] or [[array]] element, as a new line `key = "v"` after the last key-value pair;
//   - in a table that only dotted keys define (a.path = "p"), as a line `a.key = "v"` after the last line of
//     that table, whose dotted prefix is spelled as the first of those lines spells it (bare, quoted, with the
//     spaces around its dots), and which sits in the table that holds those lines, a header table or the root;
//   - in an inline table, as `, key = "v" ` after the last pair, or ` key = "v" ` in an empty one.
//
// New lines end with the line ending of the document (the first one it has). The last Key of path is the new key
// (its Index is ignored); the parent must exist, or the error wraps ErrNoParent. A parent that cannot take keys
// (a scalar, an array, a table that only a longer header implies, or dotted keys inside an inline table) wraps
// ErrNotInsertable.
func (d *Document) SetString(path []Key, v string) error {
	if len(path) == 0 {
		return fmt.Errorf("%w: empty path", ErrNoParent)
	}
	quoted, err := escape(v, `"`)
	if err != nil {
		return err
	}
	quoted = `"` + quoted + `"`
	if n, ok := d.resolve(path, true, false); ok {
		if n.val.Kind == Table || n.val.Kind == ArrayOfTables {
			return fmt.Errorf("%w: %s", ErrNotValue, path[len(path)-1].Name)
		}
		return d.splice(n.val.Span[0], n.val.Span[1], quoted)
	}
	parent, ok := d.resolve(path[:len(path)-1], true, true)
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoParent, joinNames(path[:len(path)-1]))
	}
	key, err := formatKey(path[len(path)-1].Name)
	if err != nil {
		return err
	}
	pair := key + " = " + quoted
	switch {
	case parent.val.Kind == InlineTable:
		return d.insertInline(parent, pair)
	case parent.val.Kind == Table && parent.header:
		return d.insertLine(parent.lastEnd, pair)
	case parent.val.Kind == Table && parent.dotted && parent.prefix != "":
		return d.insertLine(parent.lastEnd, parent.prefix+"."+pair)
	}
	return fmt.Errorf("%w: %s", ErrNotInsertable, joinNames(path[:len(path)-1]))
}

// ReplaceStringContents replaces the text between the quotes of the string at path with v and keeps every other
// byte, including the quotes and their style. v is escaped for the style: basic strings get TOML escapes, literal
// strings take v as is. A literal string cannot hold a quote or a control character, and the error then wraps
// ErrUnrepresentable.
func (d *Document) ReplaceStringContents(path []Key, v string) error {
	n, ok := d.resolve(path, true, false)
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoParent, joinNames(path))
	}
	if n.val.Kind != String {
		return fmt.Errorf("%w: %s", ErrNotString, joinNames(path))
	}
	start, end := n.val.Span[0], n.val.Span[1]
	delim := string(d.src[start])
	if strings.HasPrefix(string(d.src[start:end]), strings.Repeat(delim, 3)) {
		delim = strings.Repeat(delim, 3)
	}
	text, err := escape(v, delim)
	if err != nil {
		return err
	}
	if len(delim) == 3 {
		text = keepLeadingNewline(text, v)
	}
	return d.splice(start+len(delim), end-len(delim), text)
}

// keepLeadingNewline compensates for TOML trimming a newline that directly follows the opening delimiter of a
// multi-line string: text, the escaped form of v, gets one more line break when v starts with one.
func keepLeadingNewline(text, v string) string {
	if strings.HasPrefix(v, "\n") || strings.HasPrefix(v, "\r\n") {
		return "\n" + text
	}
	return text
}

// insertLine inserts a key-value line at pos, which is the start of a line or the end of the document. If the
// document ends without a line break there, the new line follows a new break and has none of its own.
func (d *Document) insertLine(pos int, pair string) error {
	eol := d.lineEnding()
	if pos > 0 && d.src[pos-1] != '\n' {
		return d.splice(pos, pos, eol+pair)
	}
	return d.splice(pos, pos, pair+eol)
}

// lineEnding is "\r\n" when the first line break of the document is one, and "\n" otherwise.
func (d *Document) lineEnding() string {
	if i := bytes.IndexByte(d.src, '\n'); i > 0 && d.src[i-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}

// insertInline inserts a pair at the end of an inline table. The whitespace after the last pair stays before the
// comma, as toml_edit leaves it; the new pair carries its own spaces.
func (d *Document) insertInline(table *node, pair string) error {
	open := table.val.Span[0]
	if table.lastEnd == open+1 {
		return d.splice(open+1, open+1, " "+pair+" ")
	}
	pos := table.lastEnd
	for pos < len(d.src) && strings.IndexByte(" \t\r\n", d.src[pos]) >= 0 {
		pos++
	}
	return d.splice(pos, pos, ", "+pair+" ")
}

// splice replaces src[start:end] with text and parses the result. The document is unchanged on error.
func (d *Document) splice(start, end int, text string) error {
	out := make([]byte, 0, len(d.src)-(end-start)+len(text))
	out = append(append(append(out, d.src[:start]...), text...), d.src[end:]...)
	root, err := index(out)
	if err != nil {
		return fmt.Errorf("edit produced invalid TOML: %w", err)
	}
	d.src, d.root = out, root
	return nil
}

func joinNames(path []Key) string {
	names := make([]string, len(path))
	for i, k := range path {
		names[i] = k.Name
	}
	return strings.Join(names, ".")
}

// formatKey writes name as a bare key when TOML allows it and as a basic string otherwise.
func formatKey(name string) (string, error) {
	bare := name != ""
	for _, r := range name {
		letter := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		if !letter && (r < '0' || r > '9') && r != '_' && r != '-' {
			bare = false
		}
	}
	if bare {
		return name, nil
	}
	text, err := escape(name, `"`)
	if err != nil {
		return "", err
	}
	return `"` + text + `"`, nil
}

// escape returns v as the contents of a string whose opening delimiter is delim: a single or triple quote of
// either kind.
func escape(v, delim string) (string, error) {
	if !utf8.ValidString(v) {
		return "", fmt.Errorf("%w: not valid UTF-8", ErrUnrepresentable)
	}
	multiline := len(delim) == 3
	literal := delim[0] == '\''
	var b strings.Builder
	for i, r := range v {
		switch {
		case literal:
			lineBreak := multiline && (r == '\n' || (r == '\r' && strings.HasPrefix(v[i:], "\r\n")))
			if r == '\'' || r == 0x7f || (r < 0x20 && r != '\t' && !lineBreak) {
				return "", fmt.Errorf("%w: %q", ErrUnrepresentable, r)
			}
			b.WriteRune(r)
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n' && multiline:
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			b.WriteString(controlEscape(r))
		default:
			b.WriteRune(r)
		}
	}
	return b.String(), nil
}

func controlEscape(r rune) string {
	switch r {
	case '\b':
		return `\b`
	case '\t':
		return `\t`
	case '\n':
		return `\n`
	case '\f':
		return `\f`
	case '\r':
		return `\r`
	}
	return fmt.Sprintf(`\u%04X`, r)
}
