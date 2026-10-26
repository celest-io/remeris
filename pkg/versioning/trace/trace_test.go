package trace_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/trace"
)

func TestDebugStrings(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		in   []string
		want string
	}{
		"nil":            {nil, `[]`},
		"empty":          {[]string{}, `[]`},
		"one":            {[]string{"a"}, `["a"]`},
		"two":            {[]string{"a", "b"}, `["a", "b"]`},
		"empty string":   {[]string{""}, `[""]`},
		"quote":          {[]string{`a"b`}, `["a\"b"]`},
		"single quote":   {[]string{"it's"}, `["it's"]`},
		"backslash":      {[]string{`a\b`}, `["a\\b"]`},
		"newline tab cr": {[]string{"a\nb\tc\rd"}, `["a\nb\tc\rd"]`},
		"nul":            {[]string{"a\x00b"}, `["a\0b"]`},
		"control":        {[]string{"\x01\x1b\x7f"}, `["\u{1}\u{1b}\u{7f}"]`},
		"unicode kept":   {[]string{"héllo ✓ 日本"}, `["héllo ✓ 日本"]`},
		"emoji kept":     {[]string{"🚀"}, `["🚀"]`},
		"c1 control":     {[]string{"\u0085"}, `["\u{85}"]`},
		"no-break space": {[]string{"\u00a0"}, `["\u{a0}"]`},
		"format char":    {[]string{"\u200b"}, `["\u{200b}"]`},
		"combining":      {[]string{"e\u0301"}, `["e\u{301}"]`},
		"line separator": {[]string{"\u2028"}, `["\u{2028}"]`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := trace.DebugStrings(tt.in); got != tt.want {
				t.Errorf("DebugStrings(%q) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

// plain prints only the message of a record, one per line, for records at or above min.
type plain struct {
	buf *bytes.Buffer
	min slog.Level
}

func (h plain) Enabled(_ context.Context, l slog.Level) bool { return l >= h.min }
func (h plain) Handle(_ context.Context, r slog.Record) error {
	h.buf.WriteString(r.Message + "\n")
	return nil
}
func (h plain) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h plain) WithGroup(string) slog.Handler      { return h }

func TestTracer(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	tr := trace.New(slog.New(plain{&buf, slog.LevelDebug}))
	if !tr.Enabled() {
		t.Fatal("Enabled() = false, want true")
	}
	tr.Debug("one")
	tr.Debug("two")
	if got, want := buf.String(), "one\ntwo\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestTracerDebugDisabled(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	tr := trace.New(slog.New(plain{&buf, slog.LevelInfo}))
	if tr.Enabled() {
		t.Error("Enabled() = true, want false when the handler drops debug")
	}
	tr.Debug("x")
	if buf.Len() != 0 {
		t.Errorf("output = %q, want none", buf.String())
	}
}

func TestSilentTracers(t *testing.T) {
	t.Parallel()
	tests := map[string]*trace.Tracer{"nil tracer": nil, "nil logger": trace.New(nil), "zero value": {}}
	for name, tr := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if tr.Enabled() {
				t.Error("Enabled() = true, want false")
			}
			tr.Debug("x")
		})
	}
}
