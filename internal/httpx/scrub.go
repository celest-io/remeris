package httpx

import (
	"errors"
	"net/url"
	"slices"
	"strings"

	"github.com/celest-io/remeris/internal/diag"
)

// Redacted replaces a secret in every message a [Scrubber] cleans.
const Redacted = "[REDACTED]"

// Scrubber removes secrets, such as a forge token, from text and from diagnostics, so that an error returned by a
// client never carries a credential, whatever the server or the transport echoed into it.
//
// For each secret it removes the text as is and its URL-escaped forms (query and path escaping). A secret derived
// from another one, such as the base64 form of "email:token" in a Basic header, is passed as its own secret.
// A Scrubber is immutable and safe for use by several goroutines. The zero value and a nil *Scrubber change nothing.
type Scrubber struct {
	replacer *strings.Replacer
}

// NewScrubber returns a Scrubber for secrets. Empty secrets are ignored.
func NewScrubber(secrets ...string) *Scrubber {
	var forms []string
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		for _, form := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
			if !slices.Contains(forms, form) {
				forms = append(forms, form)
			}
		}
	}
	if len(forms) == 0 {
		return &Scrubber{}
	}
	// strings.Replacer tries the olds in argument order at each position: the longest first, so that a secret that
	// contains another one is removed whole.
	slices.SortStableFunc(forms, func(a, b string) int { return len(b) - len(a) })
	pairs := make([]string, 0, 2*len(forms))
	for _, form := range forms {
		pairs = append(pairs, form, Redacted)
	}
	return &Scrubber{replacer: strings.NewReplacer(pairs...)}
}

// String returns text with the secrets replaced by [Redacted].
func (s *Scrubber) String(text string) string {
	if s == nil || s.replacer == nil {
		return text
	}
	return s.replacer.Replace(text)
}

// Diag returns d with the secrets removed from its message, help, link, source and related errors, and its other
// fields kept. The cause stays reachable through errors.Is and errors.As, but its text is not part of any message:
// see [scrubbedCause].
func (s *Scrubber) Diag(d diag.Diagnostic) *diag.Error {
	out := diag.New(s.String(d.Error())).
		WithCode(d.Code()).
		WithHelp(s.String(d.Help())).
		WithURL(s.String(d.URL()))
	if src := d.SourceCode(); src != "" || len(d.Labels()) > 0 {
		out = out.WithSource(s.String(src), d.Labels()...)
	}
	for _, rel := range d.Related() {
		out = out.WithRelated(s.related(rel))
	}
	if cause := errors.Unwrap(d); cause != nil {
		out = out.WithCause(scrubbedCause{msg: s.String(cause.Error()), err: cause})
	}
	return out
}

func (s *Scrubber) related(err error) error {
	var d diag.Diagnostic
	if errors.As(err, &d) {
		return s.Diag(d)
	}
	return errors.New(s.String(err.Error()))
}

// scrubbedCause is an error whose text has been cleaned and whose cause is still reachable.
//
// Unwrap exposes the original cause so that errors.Is and errors.As keep working (context.Canceled and
// context.DeadlineExceeded matter to callers). That cause may hold a secret in its own text: it must never be
// formatted, rendered or logged. Nothing in remeris prints the chain; print the error itself.
type scrubbedCause struct {
	msg string
	err error
}

func (s scrubbedCause) Error() string { return s.msg }
func (s scrubbedCause) Unwrap() error { return s.err }
