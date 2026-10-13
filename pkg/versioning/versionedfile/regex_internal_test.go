package versionedfile

import (
	"slices"
	"testing"
)

func TestSplitLinesKeepsBareCarriageReturn(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		in   string
		want []string
	}{
		"crlf lines":              {"a\r\nb\r\n", []string{"a", "b"}},
		"unterminated last line":  {"a\r\nb\r", []string{"a", "b\r"}},
		"terminated bare cr line": {"a\r\n", []string{"a"}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := splitLines(tc.in); !slices.Equal(got, tc.want) {
				t.Errorf("splitLines(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
