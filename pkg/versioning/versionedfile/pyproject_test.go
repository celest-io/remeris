package versionedfile_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	pyPath      = "beep/boop"
	pyRC        = "0.1.0-rc.0"
	pyNewRC     = "1.2.3-rc.4"
	pyPoetryRC  = "[tool.poetry]\nname = \"tester\"\nversion = \"" + pyRC + "\"\n"
	pyProjectRC = "[project]\nname = \"tester\"\nversion = \"" + pyRC + "\"\n"
	pyIndent    = "\n        [tool.poetry]\n        name = \"tester\"\n        version = \"" + pyRC + "\"\n        "
	pyHelpOne   = "Make sure [project.version] or [tool.poetry.version] is set."
	pyV200      = "2.0.0"
	pyOne       = "1.0.0"
	pyHeaders   = "[project]\nversion = \""
	pyPoetryHdr = "\"\n\n[tool.poetry]\nversion = \""
)

func newPy(t *testing.T, content string) versionedfile.File {
	t.Helper()
	f, err := versionedfile.New(versionedfile.Config{Path: pyPath, Format: versionedfile.PyProject}, content, nil)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestPyProjectVersion(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		content string
		want    string
	}{
		"poetry":               {pyPoetryRC, pyRC},
		"pep621":               {pyProjectRC, pyRC},
		"mixed":                {pyPoetryRC + "\n" + pyProjectRC, pyRC},
		"indented":             {pyIndent, pyRC},
		"inline tables":        {"project = { version = \"1.2.3\" }\n", v123},
		"literal string":       {"[project]\nversion = '1.2.3'\n", v123},
		"comments around":      {"# a\n[project] # b\nversion = \"1.2.3\" # c\n# d\n", v123},
		"dotted keys":          {"project.version = \"1.2.3\"\n", v123},
		"poetry via dotted":    {"tool.poetry.version = \"1.2.3\"\n", v123},
		"unrelated tables":     {"[build-system]\nrequires = [\"x\"]\n[project]\nversion = \"7.8.9\"\n", "7.8.9"},
		"ignores other fields": {"[project]\nname = 3\nversion = \"7.8.9\"\n[tool.black]\nline-length = 1\n", "7.8.9"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := newPy(t, tc.content).Version()
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

// pyDiag is the expected message prefix, code and help prefix of a diagnostic.
type pyDiag struct{ msg, code, help string }

var (
	pyNoVersions = pyDiag{"No versions were found in beep/boop", "remeris::pyproject::no_versions", pyHelpOne}
	pyInvalid    = pyDiag{
		"Could not deserialize beep/boop as a pyproject.toml: ",
		"remeris::pyproject::invalid",
		"remeris expects",
	}
	pyConflict = pyDiag{
		"Found conflicting versions 2.3.4 and 1.0.0 in beep/boop",
		"remeris::pyproject::inconsistent",
		"Make sure [project.version] and [tool.poetry.version] are the same.",
	}
)

func TestPyProjectErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		content string
		want    pyDiag
	}{
		"mismatch": {
			"[tool.poetry]\nname = \"tester\"\nversion = \"1.0.0\"\n\n[project]\nname = \"tester\"\nversion = \"2.3.4\"\n",
			pyConflict,
		},
		"neither":        {"[project]\nname = \"x\"\n", pyNoVersions},
		"no content":     {"", pyNoVersions},
		"other table":    {"[tool.black]\nversion = \"1.0.0\"\n", pyNoVersions},
		"invalid TOML":   {"[project\nversion = 1", pyInvalid},
		"integer":        {"[project]\nversion = 1\n", pyInvalid},
		"project string": {"project = \"x\"\n", pyInvalid},
		"tool array":     {"[[tool]]\nx = 1\n", pyInvalid},
		"poetry integer": {"[tool.poetry]\nversion = 1\n", pyInvalid},
		"bad version":    {"[project]\nversion = \"nope\"\n", pyDiag{}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, err := versionedfile.New(versionedfile.Config{Path: pyPath, Format: versionedfile.PyProject}, tc.content, nil)
			if f != nil || err == nil {
				t.Fatalf("got %v, %v", f, err)
			}
			if tc.want.code == "" {
				return
			}
			var d diag.Diagnostic
			if !errors.As(err, &d) {
				t.Fatalf("want a diagnostic, got %v", err)
			}
			if !strings.HasPrefix(d.Error(), tc.want.msg) || d.Code() != tc.want.code ||
				!strings.HasPrefix(d.Help(), tc.want.help) {
				t.Errorf("got message %q code %q help %q", d.Error(), d.Code(), d.Help())
			}
			if tc.want == pyInvalid && errors.Unwrap(err) == nil {
				t.Error("the parse error is not attached as the cause")
			}
		})
	}
}

func TestPyProjectSetVersion(t *testing.T) {
	t.Parallel()
	both := pyHeaders + pyRC + pyPoetryHdr + pyRC + "\"\n"
	tests := map[string]struct {
		content string
		steps   []string
		want    string
		diff    string
	}{
		"knope test": {
			"\n        [tool.poetry]\n        name = \"tester\"\n        version = \"" + pyRC + "\"\n        \n" +
				"        [project]\n        name = \"tester\"\n        version = \"" + pyRC + "\"\n        ",
			[]string{pyNewRC},
			"\n        [tool.poetry]\n        name = \"tester\"\n        version = \"" + pyNewRC + "\"\n        \n" +
				"        [project]\n        name = \"tester\"\n        version = \"" + pyNewRC + "\"\n        ",
			pyNewRC,
		},
		"only poetry": {pyPoetryRC, []string{pyV200}, strings.Replace(pyPoetryRC, pyRC, pyV200, 1), pyV200},
		"only pep621": {pyProjectRC, []string{pyV200}, strings.Replace(pyProjectRC, pyRC, pyV200, 1), pyV200},
		"both":        {both, []string{pyV200}, pyHeaders + pyV200 + pyPoetryHdr + pyV200 + "\"\n", pyV200},
		"longer to shorter": {pyHeaders + "10.20.30-beta.1" + pyPoetryHdr + "10.20.30-beta.1\"\n", []string{"1.0.0"},
			pyHeaders + pyOne + pyPoetryHdr + pyOne + "\"\n", pyOne},
		"shorter to longer": {pyHeaders + "1.0.0" + pyPoetryHdr + "1.0.0\"\n", []string{"10.20.30-beta.1"},
			pyHeaders + "10.20.30-beta.1" + pyPoetryHdr + "10.20.30-beta.1\"\n", "10.20.30-beta.1"},
		"literal string": {"[project]\nversion = '1.0.0'\n", []string{pyV200}, "[project]\nversion = '2.0.0'\n", pyV200},
		"comments kept": {"# top\n[project] # h\nversion = \"1.0.0\" # keep\n# end\n", []string{pyV200},
			"# top\n[project] # h\nversion = \"2.0.0\" # keep\n# end\n", pyV200},
		"inline table": {
			"project = { version = \"1.0.0\", name = \"x\" }\n",
			[]string{pyV200},
			"project = { version = \"2.0.0\", name = \"x\" }\n",
			pyV200,
		},
		"no trailing eol": {"[project]\nversion = \"1.0.0\"", []string{pyV200}, "[project]\nversion = \"2.0.0\"", pyV200},
		"CRLF": {
			"[project]\r\nversion = \"1.0.0\"\r\n",
			[]string{pyV200},
			"[project]\r\nversion = \"2.0.0\"\r\n",
			pyV200,
		},
		"multi-line string": {
			"[project]\nversion = \"\"\"1.0.0\"\"\"\n",
			[]string{pyV200},
			"[project]\nversion = \"\"\"2.0.0\"\"\"\n",
			pyV200,
		},
		"diff is replaced": {pyProjectRC, []string{"1.0.0", pyV200}, strings.Replace(pyProjectRC, pyRC, pyV200, 1), pyV200},
		"same version":     {pyProjectRC, []string{pyRC}, pyProjectRC, pyRC},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newPy(t, tc.content)
			for _, s := range tc.steps {
				var err error
				if f, err = f.SetVersion(mustVersion(t, s), nil, versionedfile.Standard); err != nil {
					t.Fatal(err)
				}
			}
			acts := f.Write()
			if len(acts) != 1 {
				t.Fatalf("got %d actions", len(acts))
			}
			if got := acts[0]; got.Path != pyPath || got.Content != tc.want || got.Diff != tc.diff {
				t.Errorf(
					"got path %q diff %q content:\n%s\nwant diff %q content:\n%s",
					got.Path,
					got.Diff,
					got.Content,
					tc.diff,
					tc.want,
				)
			}
			if v, _ := f.Version(); v.String() != tc.steps[len(tc.steps)-1] {
				t.Errorf("Version() = %s", v)
			}
		})
	}
}

func TestPyProjectFixtures(t *testing.T) {
	t.Parallel()
	// Knope's tests/prepare_release/pyproject_toml/{mixed,pep621,poetry} in and out files. Despite the names,
	// "pep621" holds only Poetry and "poetry" only PEP 621.
	tests := map[string]struct{ in, out string }{
		"mixed": {
			"[project]\nversion = \"1.0.0\"\n\n[tool.poetry]\nversion = \"1.0.0\"\n",
			"[project]\nversion = \"2.0.0\"\n\n[tool.poetry]\nversion = \"2.0.0\"\n",
		},
		"pep621": {"[tool.poetry]\nversion = \"1.0.0\"\n", "[tool.poetry]\nversion = \"2.0.0\"\n"},
		"poetry": {"[project]\nversion = \"1.0.0\"\n", "[project]\nversion = \"2.0.0\"\n"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, err := newPy(t, tc.in).SetVersion(mustVersion(t, pyV200), nil, versionedfile.Standard)
			if err != nil {
				t.Fatal(err)
			}
			acts := f.Write()
			if len(acts) != 1 || acts[0].Content != tc.out || acts[0].Diff != pyV200 {
				t.Errorf("got %v, want content:\n%s", acts, tc.out)
			}
		})
	}
}

func TestPyProjectDependency(t *testing.T) {
	t.Parallel()
	f, err := newPy(t, pyProjectRC).SetVersion(mustVersion(t, pyV200), strp("dep"), versionedfile.Standard)
	if f != nil {
		t.Errorf("got a file %v", f)
	}
	var d diag.Diagnostic
	if !errors.As(err, &d) || d.Error() != "Dependencies are not supported in boop files" {
		t.Errorf("got %v", err)
	}
	if d == nil || d.Code() != "remeris::package::unsupported_dependency" {
		t.Errorf("got %v", err)
	}
}

func TestPyProjectWriteWithoutEdit(t *testing.T) {
	t.Parallel()
	if acts := newPy(t, pyProjectRC).Write(); acts != nil {
		t.Errorf("got %v, want nil", acts)
	}
}

func TestPyProjectImmutable(t *testing.T) {
	t.Parallel()
	orig := newPy(t, pyProjectRC)
	edited, err := orig.SetVersion(mustVersion(t, pyV200), nil, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	if orig.Write() != nil {
		t.Error("the receiver gained an edit")
	}
	if v, _ := orig.Version(); v.String() != pyRC {
		t.Errorf("the receiver's version changed to %s", v)
	}
	if _, err = edited.SetVersion(mustVersion(t, "3.0.0"), nil, versionedfile.Standard); err != nil {
		t.Fatal(err)
	}
	if got := edited.Write()[0].Content; !strings.Contains(got, pyV200) {
		t.Errorf("the second call changed the first value: %q", got)
	}
}
