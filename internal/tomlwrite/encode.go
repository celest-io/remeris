package tomlwrite

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ErrInvalidUTF8 is returned when a string or a key is not valid UTF-8. TOML text is UTF-8, and Rust strings
// always are, so there is no faithful encoding of such a string.
var ErrInvalidUTF8 = errors.New("tomlwrite: string is not valid UTF-8")

// The string and key encoders are a port of toml_writer 1.1.3 (src/string.rs): TomlStringBuilder::as_default,
// TomlKeyBuilder::as_default, ValueMetrics, KeyMetrics and write_toml_value. Every string that reaches the output
// goes through them.

// style is the toml_writer Encoding enum plus the "no quotes" case of bare keys.
type style int

const (
	styleBare style = iota
	styleLiteral
	styleBasic
	styleMLLiteral
	styleMLBasic
)

// valueMetrics is ValueMetrics: what a string contains.
type valueMetrics struct {
	maxSingleQuotes int  // longest run of '
	maxDoubleQuotes int  // longest run of "
	escapeCodes     bool // a control character other than tab and newline, or DEL
	escape          bool // a backslash
	newline         bool // a line feed
}

func measureValue(s string) valueMetrics {
	var m valueMetrics
	singles, doubles := 0, 0
	for i := range len(s) {
		b := s[i]
		if b == '\'' {
			singles++
			m.maxSingleQuotes = max(m.maxSingleQuotes, singles)
		} else {
			singles = 0
		}
		if b == '"' {
			doubles++
			m.maxDoubleQuotes = max(m.maxDoubleQuotes, doubles)
		} else {
			doubles = 0
		}
		switch {
		case b == '\\':
			m.escape = true
		case b == '\t':
			// Always allowed, so neutral.
		case b == '\n':
			m.newline = true
		case isControl(b):
			m.escapeCodes = true
		}
	}
	return m
}

// isControl reports whether b is an ASCII control character that TOML does not allow raw: %x00-1F and DEL.
func isControl(b byte) bool { return b <= 0x1f || b == 0x7f }

// valueStyle is TomlStringBuilder::as_default: basic if it needs no escape, then literal, then multi-line basic,
// then multi-line literal, then escaped basic (multi-line when the text has a line feed).
func valueStyle(m valueMetrics) style {
	switch {
	case !m.escapeCodes && !m.escape && m.maxDoubleQuotes == 0 && !m.newline: // as_basic_pretty
		return styleBasic
	case !m.escapeCodes && m.maxSingleQuotes == 0 && !m.newline: // as_literal
		return styleLiteral
	case !m.escapeCodes && !m.escape && m.maxDoubleQuotes <= 2: // as_ml_basic_pretty
		return styleMLBasic
	case !m.escapeCodes && m.maxSingleQuotes <= 2: // as_ml_literal
		return styleMLLiteral
	case m.newline:
		return styleMLBasic
	default:
		return styleBasic
	}
}

// keyStyle is TomlKeyBuilder::as_default: bare, basic, literal, then escaped basic. Keys are never multi-line.
func keyStyle(s string) style {
	bare := s != ""
	var singles, doubles, escapeCodes, escape bool
	for i := range len(s) {
		b := s[i]
		if !isBareKeyByte(b) {
			bare = false
		}
		switch {
		case b == '\'':
			singles = true
		case b == '"':
			doubles = true
		case b == '\\':
			escape = true
		case b == '\t':
			// Always allowed.
		case isControl(b):
			escapeCodes = true
		}
	}
	switch {
	case bare:
		return styleBare
	case !escapeCodes && !escape && !doubles: // as_basic_pretty
		return styleBasic
	case !escapeCodes && !singles: // as_literal
		return styleLiteral
	default:
		return styleBasic
	}
}

func isBareKeyByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-' || b == '_'
}

// encodeString returns s as a TOML string value.
func encodeString(s string) (string, error) {
	if !utf8.ValidString(s) {
		return "", ErrInvalidUTF8
	}
	m := measureValue(s)
	return render(s, valueStyle(m), m.newline), nil
}

// encodeKey returns s as a TOML key (one segment of a dotted key).
func encodeKey(s string) (string, error) {
	if !utf8.ValidString(s) {
		return "", ErrInvalidUTF8
	}
	return render(s, keyStyle(s), false), nil
}

// render is write_toml_value: the delimiters, the line feed after a multi-line opener, and the escaping.
func render(s string, st style, newline bool) string {
	var delimiter string
	switch st {
	case styleBare:
		delimiter = ""
	case styleLiteral:
		delimiter = "'"
	case styleBasic:
		delimiter = `"`
	case styleMLLiteral:
		delimiter = "'''"
	case styleMLBasic:
		delimiter = `"""`
	}
	multiline := st == styleMLLiteral || st == styleMLBasic

	var sb strings.Builder
	sb.WriteString(delimiter)
	if newline && multiline {
		sb.WriteByte('\n')
	}
	if st == styleBasic || st == styleMLBasic {
		writeEscaped(&sb, s, multiline)
	} else {
		sb.WriteString(s)
	}
	sb.WriteString(delimiter)
	return sb.String()
}

// writeEscaped writes the body of a basic string. Inside a multi-line string a line feed stays raw and at most two
// double quotes in a row are raw (the third is escaped, and the count starts again after it). It works on bytes:
// every byte it changes is ASCII, so the bytes of a multi-byte character pass through untouched.
func writeEscaped(sb *strings.Builder, s string, multiline bool) {
	maxQuotes := 0
	if multiline {
		maxQuotes = 2
	}
	quotes := 0
	for i := range len(s) {
		b := s[i]
		if b == '"' {
			quotes++
			if quotes > maxQuotes {
				sb.WriteString(`\"`)
				quotes = 0
				continue
			}
			sb.WriteByte(b)
			continue
		}
		quotes = 0
		switch {
		case b == '\b':
			sb.WriteString(`\b`)
		case b == '\t':
			sb.WriteString(`\t`)
		case b == '\n' && !multiline:
			sb.WriteString(`\n`)
		case b == '\f':
			sb.WriteString(`\f`)
		case b == '\r':
			sb.WriteString(`\r`)
		case b == '\\':
			sb.WriteString(`\\`)
		case b == '\n':
			sb.WriteByte(b)
		case isControl(b):
			fmt.Fprintf(sb, `\u%04X`, b)
		default:
			sb.WriteByte(b)
		}
	}
}
