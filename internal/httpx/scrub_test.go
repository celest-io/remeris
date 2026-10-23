package httpx

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
)

const (
	abc       = "abc"
	unchanged = "unchanged"
)

func TestScrubberString(t *testing.T) {
	const secret = "s3cr3t/+ blob=" //nolint:gosec // G101: a sentinel, not a credential
	tests := map[string]struct {
		secrets []string
		in      string
		want    string
	}{
		"plain":            {secrets: []string{abc}, in: "x " + abc + " y " + abc, want: "x [REDACTED] y [REDACTED]"},
		"query escaped":    {secrets: []string{secret}, in: "?t=" + url.QueryEscape(secret), want: "?t=[REDACTED]"},
		"path escaped":     {secrets: []string{secret}, in: "/" + url.PathEscape(secret) + "/x", want: "/[REDACTED]/x"},
		"lowercase query":  {secrets: []string{secret}, in: strings.ToLower(url.QueryEscape(secret)), want: "[REDACTED]"},
		"lowercase path":   {secrets: []string{secret}, in: "/" + strings.ToLower(url.PathEscape(secret)), want: "/[REDACTED]"},
		"empty secret":     {secrets: []string{""}, in: unchanged, want: unchanged},
		"no secret":        {in: unchanged, want: unchanged},
		"longest first":    {secrets: []string{"abc", "abcdef"}, in: "abcdef", want: "[REDACTED]"},
		"several secrets":  {secrets: []string{"one", "two"}, in: "one two", want: "[REDACTED] [REDACTED]"},
		"secret not found": {secrets: []string{abc}, in: "xyz", want: "xyz"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := NewScrubber(tc.secrets...).String(tc.in); got != tc.want {
				t.Errorf("String(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNilScrubberChangesNothing(t *testing.T) {
	var s *Scrubber
	if got := s.String("abc"); got != "abc" {
		t.Errorf("String = %q", got)
	}
}

func TestScrubberDiagKeepsFieldsAndCause(t *testing.T) {
	s := NewScrubber("tok")
	original := diag.New("message tok").WithCode("a::b").WithHelp("help tok").
		WithRelated(errors.New("related tok")).WithCause(context.Canceled)

	got := s.Diag(original)

	if got.Error() != "message [REDACTED]" || got.Code() != "a::b" || got.Help() != "help [REDACTED]" {
		t.Errorf("got %q %q %q", got.Error(), got.Code(), got.Help())
	}
	if rel := got.Related(); len(rel) != 1 || rel[0].Error() != "related [REDACTED]" {
		t.Errorf("related = %v", rel)
	}
	if !errors.Is(got, context.Canceled) {
		t.Error("the cause should stay reachable")
	}
}
