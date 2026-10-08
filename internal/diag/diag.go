// Package diag defines remeris' diagnostic errors and renders them for the terminal (spec F3.3, D4).
//
// A diagnostic is an error with optional help text, a code, a docs URL, labelled source spans and
// nested related errors. Version 1 renders only the message, the help text and the related errors.
// The other accessors exist so snippets and links can be switched on later without touching every
// error.
package diag

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

// Label marks the byte range [Start, End) of a diagnostic's source code.
type Label struct {
	Start, End int
	Text       string
}

// Diagnostic is an error that carries extra information for the user.
type Diagnostic interface {
	error
	Code() string
	Help() string
	URL() string
	Labels() []Label
	SourceCode() string
	Related() []error
}

// Error is the stock Diagnostic implementation. Build one with New or Newf and chain the With
// methods, which return modified copies.
type Error struct {
	msg     string
	code    string
	help    string
	url     string
	source  string
	labels  []Label
	related []error
	cause   error
}

// New returns a diagnostic with the given message.
func New(msg string) *Error { return &Error{msg: msg} }

// Newf returns a diagnostic with a formatted message. A %w verb sets the cause, as in fmt.Errorf.
func Newf(format string, args ...any) *Error {
	wrapped := fmt.Errorf(format, args...)
	return &Error{msg: wrapped.Error(), cause: errors.Unwrap(wrapped)}
}

func (e *Error) Error() string      { return e.msg }
func (e *Error) Unwrap() error      { return e.cause }
func (e *Error) Code() string       { return e.code }
func (e *Error) Help() string       { return e.help }
func (e *Error) URL() string        { return e.url }
func (e *Error) Labels() []Label    { return e.labels }
func (e *Error) SourceCode() string { return e.source }
func (e *Error) Related() []error   { return e.related }
func (e *Error) clone() *Error      { c := *e; return &c }

// WithCode sets the machine-readable code, such as "remeris::step_not_implemented".
func (e *Error) WithCode(code string) *Error { c := e.clone(); c.code = code; return c }

// WithHelp sets the help text.
func (e *Error) WithHelp(help string) *Error { c := e.clone(); c.help = help; return c }

// WithURL sets the documentation link.
func (e *Error) WithURL(url string) *Error { c := e.clone(); c.url = url; return c }

// WithSource attaches source code and labelled spans pointing into it.
func (e *Error) WithSource(source string, labels ...Label) *Error {
	c := e.clone()
	c.source, c.labels = source, labels
	return c
}

// WithRelated appends nested errors, shown beneath this one.
func (e *Error) WithRelated(errs ...error) *Error {
	c := e.clone()
	c.related = append(append([]error(nil), e.related...), errs...)
	return c
}

// WithCause sets the wrapped error for errors.Is and errors.As. It is not rendered; use
// WithRelated for that.
func (e *Error) WithCause(cause error) *Error { c := e.clone(); c.cause = cause; return c }

// Render writes err to w:
//
//	Error: {message}
//	  help: {help}
//	  ╰─▶ {related message}
//
// Related errors nest, each level indented under the arrow of its parent. Errors that are not
// diagnostics print their message only.
func Render(w io.Writer, err error) {
	var b strings.Builder
	b.WriteString("Error: ")
	renderNode(&b, err, "  ")
	_, _ = io.WriteString(w, b.String())
}

// renderNode writes err's message (the caller has already written any prefix), then its help and
// related errors, with continuation lines starting with indent.
func renderNode(b *strings.Builder, err error, indent string) {
	b.WriteString(strings.ReplaceAll(err.Error(), "\n", "\n"+indent))
	b.WriteByte('\n')
	d, ok := err.(Diagnostic)
	if !ok {
		return
	}
	if help := d.Help(); help != "" {
		b.WriteString(indent + "help: " + strings.ReplaceAll(help, "\n", "\n"+indent+"      ") + "\n")
	}
	for _, rel := range d.Related() {
		b.WriteString(indent + "╰─▶ ")
		renderNode(b, rel, indent+"    ")
	}
}
