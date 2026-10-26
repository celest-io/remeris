// Package trace carries the debug lines Knope prints with --verbose through the pure versioning core.
//
// The core does no I/O: the caller passes a Tracer built on its own *slog.Logger, and the core calls Debug at the
// point where Knope calls debug!, so the lines come out in the same order. A nil *Tracer is valid and prints
// nothing; pass nil wherever Knope does not log.
package trace

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"unicode"
)

// Tracer writes debug messages to a logger. The nil *Tracer discards them.
type Tracer struct {
	logger *slog.Logger
}

// New returns a Tracer that logs at debug level to l. A nil logger gives the nil Tracer, which prints nothing.
func New(l *slog.Logger) *Tracer {
	if l == nil {
		return nil
	}
	return &Tracer{logger: l}
}

// Debug logs msg at debug level. It does nothing on a nil Tracer.
func (t *Tracer) Debug(msg string) {
	if t == nil || t.logger == nil {
		return
	}
	t.logger.Debug(msg)
}

// Debugf formats like fmt.Sprintf and logs the result at debug level. It formats nothing on a nil Tracer.
func (t *Tracer) Debugf(format string, args ...any) {
	if t == nil || t.logger == nil {
		return
	}
	t.logger.Debug(fmt.Sprintf(format, args...))
}

// DebugStrings prints the strings like Rust's {:?} of a slice of strings: ["a", "b"], and [] when empty. Each
// string is escaped like Rust's str Debug, which differs from Go's %q.
func DebugStrings(items []string) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, s := range items {
		if i > 0 {
			b.WriteString(", ")
		}
		writeDebugString(&b, s)
	}
	b.WriteByte(']')
	return b.String()
}

// writeDebugString writes s quoted with Rust's str Debug rules: \t \r \n \" \\ and \0 are escaped by name, a
// character that is not printable or extends a grapheme becomes \u{hex}, everything else is kept as it is. The
// single quote is not escaped in a string.
func writeDebugString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		case '\n':
			b.WriteString(`\n`)
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case 0:
			b.WriteString(`\0`)
		default:
			if isGraphemeExtend(r) || !unicode.IsPrint(r) {
				b.WriteString(`\u{` + strconv.FormatInt(int64(r), 16) + `}`)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}

// isGraphemeExtend is the Unicode Grapheme_Extend property: Me, Mn and Other_Grapheme_Extend.
func isGraphemeExtend(r rune) bool {
	return unicode.In(r, unicode.Me, unicode.Mn, unicode.Other_Grapheme_Extend)
}
