package versionedfile

import (
	"errors"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

// TestPubSpecInsertVersion covers the branch that is unreachable through New: a file without a version key.
func TestPubSpecInsertVersion(t *testing.T) {
	t.Parallel()
	const newVersion = "1.2.3-rc.4"
	tests := map[string]struct{ in, want string }{
		"insert at end":          {"name: x\n", "name: x\nversion: " + newVersion + "\n"},
		"insert no final line":   {"name: x", "name: x\nversion: " + newVersion + "\n"},
		"insert CRLF":            {"name: x\r\ny: 1\r\n", "name: x\r\ny: 1\r\nversion: " + newVersion + "\r\n"},
		"insert CRLF no final":   {"name: x\r\ny: 1", "name: x\r\ny: 1\r\nversion: " + newVersion + "\r\n"},
		"insert keeps nested":    {"a:\n  version: 1.0.0\n", "a:\n  version: 1.0.0\nversion: " + newVersion + "\n"},
		"insert after a comment": {"name: x\n# done\n", "name: x\n# done\nversion: " + newVersion + "\n"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := pubSpecFile{path: "pubspec.yaml", raw: tc.in}
			out, err := f.SetVersion(semver.MustParse(newVersion), nil, Standard)
			if err != nil {
				t.Fatal(err)
			}
			got := out.(pubSpecFile) //nolint:forcetypeassert // SetVersion of a pubSpecFile returns one
			if got.raw != tc.want || got.diff.String() != newVersion {
				t.Errorf("got %q, %q; want %q", got.raw, got.diff.String(), tc.want)
			}
		})
	}
	t.Run("insert refuses a flow mapping", func(t *testing.T) {
		t.Parallel()
		_, err := pubSpecFile{path: "pubspec.yaml", raw: "{a: 1}\n"}.SetVersion(semver.MustParse(newVersion), nil, Standard)
		var d diag.Diagnostic
		if !errors.As(err, &d) || d.Code() != codePubSpecSerialize {
			t.Errorf("got %v", err)
		}
	})
}

const offsetABC = "abc"

func TestYAMLOffset(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		src       string
		line, col int
		want      int
		ok        bool
	}{
		"offset first":           {offsetABC, 1, 1, 0, true},
		"offset column":          {offsetABC, 1, 3, 2, true},
		"offset end":             {offsetABC, 1, 4, 3, true},
		"offset past end":        {offsetABC, 1, 5, 0, false},
		"offset second line":     {"a\nbc", 2, 2, 3, true},
		"offset CRLF":            {"a\r\nbc", 2, 2, 4, true},
		"offset CR":              {"a\rbc", 2, 2, 3, true},
		"offset NEL":             {"a\u0085bc", 2, 2, 4, true},
		"offset LS":              {"a\u2028bc", 2, 2, 5, true},
		"offset PS":              {"a\u2029bc", 2, 2, 5, true},
		"offset missing line":    {"a\nb", 3, 1, 0, false},
		"offset BOM":             {"\xef\xbb\xbfab", 1, 2, 4, true},
		"offset runes":           {"éü: x", 1, 5, 6, true},
		"offset tab is one":      {"\tx", 1, 2, 1, true},
		"offset 4-byte rune":     {"😀x", 1, 2, 4, true},
		"offset line then runes": {"é\nüx", 2, 2, 5, true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := yamlOffset(tc.src, tc.line, tc.col)
			if ok != tc.ok || (ok && got != tc.want) {
				t.Errorf("got %d, %v; want %d, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}
