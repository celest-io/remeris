package versionedfile_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/semver"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	pubspecPath    = "pubspec.yaml"
	pubspecOld     = "1.0.0"
	pubspecNew     = "1.2.3-rc.4"
	pubspecDeser   = "remeris::pubspec_yaml::deserialize"
	pubspecSerial  = "remeris::pubspec_yaml::serialize"
	pubspecErrPre  = "Error deserializing pubspec.yaml: "
	pubspecVerLine = "version: "

	pubspecKey        = "version"
	pubspecNotMapping = "YAML was not a mapping"
	pubspecNotString  = "YAML `version` field was not a string"
)

func pubspecNewFile(content string) (versionedfile.File, error) {
	return versionedfile.New(versionedfile.Config{Path: pubspecPath, Format: versionedfile.PubSpec}, content, nil)
}

func mustPubspec(t *testing.T, content string) versionedfile.File {
	t.Helper()
	f, err := pubspecNewFile(content)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// Knope's fixture: tests/prepare_release/pubspec_yaml.
const pubspecFixture = "# Copied from https://dart.dev/tools/pub/pubspec#example\n" +
	"name: newtify\n" +
	"description: >-\n" +
	"  Have you been turned into a newt?  Would you like to be?\n" +
	"  This package can help. It has all of the\n" +
	"  newt-transmogrification functionality you have been looking\n" +
	"  for.\n" +
	"version: 1.0.0\n" +
	"homepage: https://example-pet-store.com/newtify\n" +
	"\n" +
	"environment:\n" +
	"  sdk: '>=2.12.0 <3.0.0'\n" +
	"\n" +
	"dev_dependencies:\n" +
	"  test: '>=1.15.0 <2.0.0'\n"

func TestPubspecVersion(t *testing.T) {
	t.Parallel()
	got, err := mustPubspec(t, pubspecFixture).Version()
	if err != nil || got.String() != pubspecOld {
		t.Errorf("got %v, %v", got, err)
	}
}

// TestPubspecSplice checks the splice invariant: the output is the input with the bytes of the version replaced, and
// nothing else. Each case gives the text before the version, the version as written, and the text after it, so the
// span is known independently of the code under test.
func TestPubspecSplice(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ before, old, after string }{
		"splice plain":                             {"name: x\n" + pubspecVerLine, pubspecOld, "\nhomepage: y\n"},
		"splice no final newline":                  {pubspecVerLine, pubspecOld, ""},
		"splice trailing comment kept":             {pubspecVerLine, pubspecOld, " # keep this\nname: x\n"},
		"splice single quotes kept":                {pubspecVerLine + "'", pubspecOld, "' # c\nname: x\n"},
		"splice double quotes kept":                {pubspecVerLine + "\"", pubspecOld, "\"\n"},
		"splice quoted with apostrophe in comment": {pubspecVerLine + "\"", pubspecOld, "\" # it's\n"},
		"splice CRLF":                              {"name: é\r\n" + pubspecVerLine, pubspecOld, "\r\nz: 1\r\n"},
		"splice CR only":                           {"x: 1\r" + pubspecVerLine, pubspecOld, "\rz: 1\r"},
		"splice Unicode line break":                {"\u2028x: 1\n" + pubspecVerLine, pubspecOld, "\n"},
		"splice BOM":                               {"\xef\xbb\xbf" + pubspecVerLine, pubspecOld, "\n"},
		"splice BOM and CRLF":                      {"\xef\xbb\xbfname: x\r\n" + pubspecVerLine, pubspecOld, "\r\n"},
		"splice tab after colon":                   {"version:\t", pubspecOld, "\n"},
		"splice many spaces":                       {"version:     ", pubspecOld, "   \n"},
		"splice multibyte earlier":                 {"名前: 日本語\nversion: ", pubspecOld, " # é\n"},
		"splice multibyte same line":               {"{é: ü, version: ", pubspecOld, ", z: 1}\n"},
		"splice flow mapping":                      {"{version: ", pubspecOld, "}\n"},
		"splice quoted key":                        {"\"version\": ", pubspecOld, "\n"},
		"splice explicit key":                      {"? version\n: ", pubspecOld, "\n"},
		"splice prerelease":                        {pubspecVerLine, "1.0.0-rc.1+build.5", "\n"},
		"splice flutter build number":              {pubspecVerLine, "1.0.0+1", "\n"},
		"splice document start":                    {"---\nname: x\n" + pubspecVerLine, pubspecOld, "\n...\n"},
		"splice fixture": {
			strings.SplitAfter(pubspecFixture, pubspecVerLine)[0],
			pubspecOld,
			strings.SplitAfter(pubspecFixture, pubspecOld)[1],
		},
		"nested version before is untouched": {
			"dependencies:\n  version: 9.9.9\n" + pubspecVerLine, pubspecOld, "\n",
		},
		"nested version after is untouched": {
			pubspecVerLine, pubspecOld, "\ndeps:\n  version: 1.0.0\n  inner:\n    version: 1.0.0\n",
		},
		"indented key first, top level second": {
			"a:\n  version: 1.0.0\n  b: 2\n" + pubspecVerLine, pubspecOld, "\n",
		},
		"value repeated in another key": {"other: 1.0.0\n" + pubspecVerLine, pubspecOld, "\n"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := tc.before + tc.old + tc.after
			f, err := mustPubspec(t, in).SetVersion(mustVersion(t, pubspecNew), nil, versionedfile.Standard)
			if err != nil {
				t.Fatal(err)
			}
			got, diff := lockWrite(t, f)
			if want := tc.before + pubspecNew + tc.after; got != want || diff != pubspecNew {
				t.Errorf("got %q, %q; want %q", got, diff, want)
			}
			if v, err := f.Version(); err != nil || v.String() != pubspecNew {
				t.Errorf("Version() = %v, %v", v, err)
			}
			again, err := pubspecNewFile(got)
			if err != nil {
				t.Fatal(err)
			}
			if v, err := again.Version(); err != nil || v.String() != pubspecNew {
				t.Errorf("re-read version = %v, %v", v, err)
			}
		})
	}
}

func TestPubspecSetVersionKnopeUnit(t *testing.T) {
	t.Parallel()
	f, err := mustPubspec(t, pubspecFixture).SetVersion(mustVersion(t, pubspecNew), nil, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	got, diff := lockWrite(t, f)
	if want := strings.Replace(pubspecFixture, pubspecOld, pubspecNew, 1); got != want || diff != pubspecNew {
		t.Errorf("got %q, %q", got, diff)
	}
}

func TestPubspecIdentityIdempotenceAndImmutability(t *testing.T) {
	t.Parallel()
	in := "name: x\nversion: '1.0.0' # keep\r\nz: 1\n"
	f := mustPubspec(t, in)
	same, err := f.SetVersion(mustVersion(t, pubspecOld), nil, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := lockWrite(t, same); got != in {
		t.Errorf("SetVersion(current) = %q, want the input", got)
	}
	once, err := f.SetVersion(mustVersion(t, pubspecNew), nil, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	twice, err := once.SetVersion(mustVersion(t, pubspecNew), nil, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := lockWrite(t, once)
	b, diff := lockWrite(t, twice)
	if a != b || diff != pubspecNew {
		t.Errorf("not idempotent: %q then %q (diff %q)", a, b, diff)
	}
	if f.Write() != nil || f.Path() != pubspecPath {
		t.Error("SetVersion changed its receiver")
	}
	if v, _ := f.Version(); v.String() != pubspecOld {
		t.Errorf("receiver version changed to %v", v)
	}
}

func TestPubspecDependencyIgnored(t *testing.T) {
	t.Parallel()
	f, err := mustPubspec(t, "version: 1.0.0\n").SetVersion(mustVersion(t, pubspecNew), strp("dep"), versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := lockWrite(t, f); got != pubspecVerLine+pubspecNew+"\n" {
		t.Errorf("got %q", got)
	}
}

func TestPubspecLoadErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ content, detail string }{
		"load empty":          {"", "Expected a single YAML document, found 0"},
		"load only a comment": {"# nothing\n", "Expected a single YAML document, found 0"},
		"load two documents":  {"version: 1.0.0\n---\nversion: 1.0.0\n", "Expected a single YAML document, found 2"},
		"load three documents": {
			"a: 1\n---\nb: 2\n---\nc: 3\n", "Expected a single YAML document, found 3",
		},
		"load sequence":         {"- version: 1.0.0\n", pubspecNotMapping},
		"load scalar":           {"1.0.0\n", pubspecNotMapping},
		"load empty document":   {"---\n", pubspecNotMapping},
		"load bare marker":      {"---", pubspecNotMapping},
		"load timestamp":        {"version: 2001-01-01\n", pubspecNotString},
		"load no version":       {"name: x\n", "YAML did not contain a top-level `version` field"},
		"load only nested":      {"a:\n  version: 1.0.0\n", "YAML did not contain a top-level `version` field"},
		"load version in a seq": {"- a\n", pubspecNotMapping},
		"load float":            {"version: 1.0\n", pubspecNotString},
		"load int":              {"version: 1\n", pubspecNotString},
		"load null":             {"version:\n", pubspecNotString},
		"load sequence value":   {"version: [1.0.0]\n", pubspecNotString},
		"load alias": {
			"a: &v 1.0.0\nversion: *v\n", pubspecNotString,
		},
		"load duplicate": {
			"version: 1.0.0\nversion: 2.0.0\n", "YAML contained more than one top-level `version` field",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := pubspecNewFile(tc.content)
			var d diag.Diagnostic
			if !errors.As(err, &d) || d.Error() != pubspecErrPre+tc.detail || d.Code() != pubspecDeser {
				t.Fatalf("got %v", err)
			}
			if !strings.Contains(d.Help(), "top level `version` property") {
				t.Errorf("help = %q", d.Help())
			}
		})
	}
}

func TestPubspecLoadInvalid(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"invalid yaml":      "a: [\n",
		"invalid utf-8":     "version: \xff\n",
		"tab indentation":   "a:\n\tb: 1\nversion: 1.0.0\n",
		"unterminated text": "version: \"1.0.0\n",
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := pubspecNewFile(content)
			var d diag.Diagnostic
			if !errors.As(err, &d) || d.Code() != pubspecDeser || !strings.HasPrefix(err.Error(), pubspecErrPre) {
				t.Errorf("got %v", err)
			}
		})
	}
	if _, err := pubspecNewFile("version: nope\n"); err == nil {
		t.Error("an invalid version must fail")
	}
}

func TestPubspecAliasBombIsCheap(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	b.WriteString("a: &a [x, x, x, x, x, x, x, x, x]\n")
	prev := "a"
	for _, name := range []string{"b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n"} {
		b.WriteString(
			name + ": &" + name + " [*" + prev + ", *" + prev + ", *" + prev + ", *" + prev + ", *" + prev + ", *" + prev + "]\n",
		)
		prev = name
	}
	b.WriteString(pubspecVerLine + pubspecOld + "\n")
	done := make(chan error, 1)
	go func() {
		f, err := pubspecNewFile(b.String())
		if err == nil {
			_, err = f.SetVersion(mustVersion(t, pubspecNew), nil, versionedfile.Standard)
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("aliases were expanded")
	}
}

// TestPubspecRefusesToCorrupt covers files that load but whose version cannot be replaced in place.
func TestPubspecRefusesToCorrupt(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"refuse folded block scalar":  "version: >-\n  1.0.0\n",
		"refuse literal block scalar": "version: |-\n  1.0.0\nname: x\n",
		"refuse anchor":               "version: &v 1.0.0\n",
		"refuse tag":                  "version: !!str 1.0.0\n",
		"refuse escapes":              "version: \"1\\x2e0.0\"\n",
		"refuse quoted over lines":    "version: \"1.0.0\n  \"\n",
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, err := pubspecNewFile(content)
			if err != nil {
				// A value that cannot be a version is refused at load time, which is as good.
				return
			}
			_, err = f.SetVersion(mustVersion(t, pubspecNew), nil, versionedfile.Standard)
			var d diag.Diagnostic
			if !errors.As(err, &d) || d.Code() != pubspecSerial ||
				!strings.HasPrefix(err.Error(), "Could not update pubspec.yaml: ") {
				t.Errorf("got %v", err)
			}
			if f.Write() != nil {
				t.Error("a refused edit must not leave a pending write")
			}
		})
	}
}

func TestPubspecLargeFileIsLinear(t *testing.T) {
	t.Parallel()
	const lines = 400_000
	var b strings.Builder
	b.WriteString(pubspecVerLine + "'" + pubspecOld + "'\n")
	for range lines {
		b.WriteString("dependency_name: ^1.0.0 # note\n")
	}
	b.WriteString("tail:\n  version: 1.0.0\n")
	in := b.String()
	start := time.Now()
	f, err := pubspecNewFile(in)
	if err != nil {
		t.Fatal(err)
	}
	next, err := f.SetVersion(mustVersion(t, pubspecNew), nil, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := lockWrite(t, next)
	if want := pubspecVerLine + "'" + pubspecNew + "'" + in[len(pubspecVerLine+"'"+pubspecOld+"'"):]; got != want {
		t.Error("the large file was not spliced as expected")
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Errorf("took %v", elapsed)
	}
}

// pubspecTree parses the first YAML document of content and returns it with the value node of its top-level version.
func pubspecTree(content string) (root, version *yaml.Node, err error) {
	var doc yaml.Node
	if err := yaml.NewDecoder(strings.NewReader(content)).Decode(&doc); err != nil {
		return nil, nil, err
	}
	if len(doc.Content) == 1 && doc.Content[0].Kind == yaml.MappingNode {
		m := doc.Content[0]
		for i := 0; i+1 < len(m.Content); i += 2 {
			if m.Content[i].Value == pubspecKey && m.Content[i].Tag == "!!str" {
				version = m.Content[i+1]
			}
		}
	}
	return &doc, version, nil
}

// pubspecTreeDiff returns the first difference between two node trees (kind, style, tag, value, anchor, comments and
// children), or "". The node skip is compared without its value. Positions are ignored: they move with the edit.
func pubspecTreeDiff(a, b, skip *yaml.Node) string {
	if a.Kind != b.Kind || a.Style != b.Style || a.Tag != b.Tag || a.Anchor != b.Anchor ||
		a.HeadComment != b.HeadComment || a.LineComment != b.LineComment || a.FootComment != b.FootComment {
		return "node differs: " + a.Value + " / " + b.Value
	}
	if a != skip && a.Value != b.Value {
		return "value differs: " + a.Value + " / " + b.Value
	}
	if (a.Alias == nil) != (b.Alias == nil) || (a.Alias != nil && a.Alias.Anchor != b.Alias.Anchor) {
		return "alias differs"
	}
	if len(a.Content) != len(b.Content) {
		return "children differ"
	}
	for i := range a.Content {
		if d := pubspecTreeDiff(a.Content[i], b.Content[i], skip); d != "" {
			return d
		}
	}
	return ""
}

func FuzzPubspecSetVersion(f *testing.F) {
	seeds := []string{
		pubspecFixture,
		"version: 1.0.0\n",
		"version: '1.0.0' # c\r\nname: é\r\n",
		"\xef\xbb\xbfversion: \"1.0.0\"\n",
		"{version: 1.0.0, a: [b]}\n",
		"a:\n  version: 1.0.0\nversion: 2.0.0+1\n",
		"version: >-\n  1.0.0\n",
		"? version\n: 1.0.0\n",
		"\u2028a: 1\rversion: 1.0.0\u2029",
		"a: &v 1.0.0\nversion: *v\n",
		"---\nversion: 1.0.0\n---\nversion: 1.0.0\n",
		"version: !!str 1.0.0\n",
		"version:\t\"1.0.0\"\n",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	target, err := semver.Parse(pubspecNew)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, in string) {
		file, err := pubspecNewFile(in)
		if err != nil {
			return
		}
		next, err := file.SetVersion(target, nil, versionedfile.Standard)
		if err != nil {
			return
		}
		acts := next.Write()
		if len(acts) != 1 {
			t.Fatalf("got %d actions", len(acts))
		}
		out := acts[0].Content
		inRoot, inVersion, err := pubspecTree(in)
		if err != nil || inVersion == nil {
			t.Fatalf("input tree: %v", err)
		}
		outRoot, outVersion, err := pubspecTree(out)
		if err != nil || outVersion == nil {
			t.Fatalf("output tree: %v (%q)", err, out)
		}
		if outVersion.Value != pubspecNew {
			t.Fatalf("version is %q (%q)", outVersion.Value, out)
		}
		if d := pubspecTreeDiff(inRoot, outRoot, inVersion); d != "" {
			t.Fatalf("%s: %q -> %q", d, in, out)
		}
		again, err := pubspecNewFile(out)
		if err != nil {
			t.Fatalf("output does not load: %v (%q)", err, out)
		}
		if v, err := again.Version(); err != nil || v.String() != pubspecNew {
			t.Fatalf("output reads back %v, %v (%q)", v, err, out)
		}
	})
}
