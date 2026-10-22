package versionedfile_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const gleamPath = "gleam.toml"

func newGleam(t *testing.T, content string) versionedfile.File {
	t.Helper()
	f, err := versionedfile.New(versionedfile.Config{Path: gleamPath, Format: versionedfile.Gleam}, content, nil)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestGleamSetVersion(t *testing.T) {
	t.Parallel()
	const rc = "1.2.3-rc.4"
	tests := map[string]struct {
		content string
		want    string
	}{
		// Knope's unit test.
		"indented": {"\n        version = \"0.1.0-rc.0\"\n        ", "\n        version = \"" + rc + "\"\n        "},
		"comments and tables stay": {
			"# c\nname = \"x\" # n\nversion   =   '0.1.0' # keep\n\n[dependencies]\nversion = \"9\"\n",
			"# c\nname = \"x\" # n\nversion   =   \"" + rc + "\" # keep\n\n[dependencies]\nversion = \"9\"\n",
		},
		"CRLF line endings": {"name = \"x\"\r\nversion = \"1\"\r\n", "name = \"x\"\r\nversion = \"" + rc + "\"\r\n"},
		"not a string":      {"version = 3\n", "version = \"" + rc + "\"\n"},
		"no version key":    {"name = \"x\"\n\n[deps]\na = 1\n", "name = \"x\"\nversion = \"" + rc + "\"\n\n[deps]\na = 1\n"},
		"missing, CRLF":     {"name = \"x\"\r\n[deps]\r\n", "name = \"x\"\r\nversion = \"" + rc + "\"\r\n[deps]\r\n"},
		"missing, empty":    {"", "version = \"" + rc + "\"\n"},
		"missing, no final": {"name = \"x\"", "name = \"x\"\nversion = \"" + rc + "\""},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, err := newGleam(t, tc.content).SetVersion(mustVersion(t, rc), strp("ignored"), versionedfile.Standard)
			if err != nil {
				t.Fatal(err)
			}
			got, diff := lockWrite(t, f)
			if got != tc.want || diff != "version = "+rc {
				t.Errorf("got %q, %q; want %q", got, diff, tc.want)
			}
		})
	}
}

func TestGleamIdempotentAndImmutable(t *testing.T) {
	t.Parallel()
	content := "name = \"x\"\nversion = \"1.0.0\" # keep\n"
	f := newGleam(t, content)
	next, err := f.SetVersion(mustVersion(t, "1.0.0"), nil, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := lockWrite(t, next); got != content {
		t.Errorf("got %q, want the input", got)
	}
	if f.Write() != nil || f.Path() != gleamPath {
		t.Error("SetVersion changed its receiver")
	}
}

func TestGleamVersion(t *testing.T) {
	t.Parallel()
	got, err := newGleam(t, "version = \"1.2.3\"\n[a]\nversion = \"9.9.9\"\n").Version()
	if err != nil || got.String() != v123 {
		t.Errorf("got %v, %v", got, err)
	}
	missing := "gleam.toml was missing required property version"
	tests := map[string]string{
		"missing":       "name = \"x\"\n",
		"only in table": "[a]\nversion = \"1.0.0\"\n",
		"not a string":  "version = 1\n",
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := newGleam(t, content).Version()
			var d diag.Diagnostic
			if !errors.As(err, &d) || d.Error() != missing || d.Code() != "remeris::gleam::missing_property" {
				t.Errorf("got %v", err)
			}
		})
	}
	if _, err := newGleam(t, "version = \"nope\"\n").Version(); err == nil {
		t.Error("an invalid version must fail")
	}
}

func TestGleamInvalidTOML(t *testing.T) {
	t.Parallel()
	_, err := versionedfile.New(versionedfile.Config{Path: gleamPath, Format: versionedfile.Gleam}, "version = ", nil)
	var d diag.Diagnostic
	if !errors.As(err, &d) || d.Code() != "remeris::gleam::toml" ||
		!strings.HasPrefix(err.Error(), "Invalid TOML in gleam.toml: ") {
		t.Errorf("got %v", err)
	}
}
