package denolock

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	emptyObject = "{}"
	emptyArray  = "[]"
	jsonNull    = "null"
	sectionJSR  = "jsr"

	msgMissingZzz = "npm package 'zzz' was not found and could not have its version resolved"
)

// splitSections splits a deno_lockfile spec file (tests/specs/**.txt) into the bodies of its sections, parsed as
// tests/helpers/mod.rs does. ok is false when a section does not start with a "# " header.
func splitSections(text string) (sections []string, ok bool) {
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); {
		if !strings.HasPrefix(lines[i], "# ") {
			return sections, false
		}
		i++
		var body strings.Builder
		for i < len(lines) {
			body.WriteString(lines[i])
			body.WriteByte('\n')
			i++
			if i < len(lines) && strings.HasPrefix(lines[i], "#") {
				break
			}
		}
		sections = append(sections, strings.TrimRight(body.String(), "\n")+"\n")
	}
	return sections, true
}

// specSection is a lockfile of a spec file: its index, and whether it is a section Deno's printer wrote.
type specSection struct {
	index  int
	text   string
	output bool
}

// lockfileSections returns the sections of a spec file that hold a lockfile. In config_changes the first is the
// "original" and each "change" is followed by its "output" (sections 0, 2, 4...); a transforms file is an "original"
// and its "output" (sections 0 and 1).
func lockfileSections(t *testing.T, path, text string) []specSection {
	t.Helper()
	sections, ok := splitSections(text)
	if !ok {
		t.Fatal("expected a section header")
	}
	if strings.HasPrefix(path, "transforms/") {
		if len(sections) != 2 {
			t.Fatalf("%s: %d sections, want original and output", path, len(sections))
		}
		return []specSection{{0, sections[0], false}, {1, sections[1], true}}
	}
	var out []specSection
	for i := 0; i < len(sections); i += 2 {
		out = append(out, specSection{i, sections[i], i > 0})
	}
	return out
}

func versionOf(t *testing.T, text string) string {
	t.Helper()
	root, err := ParseJSON([]byte(text))
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	v, _ := Version(root)
	return v
}

// TestRoundTripSpecs reads every version 5 lockfile of the deno_lockfile spec files and prints it again. The outputs of
// the specs are what Deno's printer wrote, so they must come back byte for byte; the inputs were written by hand and
// only need to be a fixed point after one round. The only lockfiles skipped are originals of version 3 and 4; the test
// lists them and fails if an output, or anything else, is skipped.
func TestRoundTripSpecs(t *testing.T) {
	t.Parallel()
	var outputs, transformOutputs, inputs int
	var skipped []string
	fsys := os.DirFS("testdata/specs")
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".txt" {
			return err
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		for _, sec := range lockfileSections(t, path, string(data)) {
			name := path + "#" + strconv.Itoa(sec.index)
			if v := versionOf(t, sec.text); v != "5" {
				if sec.output {
					t.Errorf("%s: an output of version %q would be skipped", name, v)
				}
				skipped = append(skipped, name+" (version "+v+")")
				continue
			}
			switch {
			case !sec.output:
				inputs++
			case strings.HasPrefix(path, "transforms/"):
				outputs++
				transformOutputs++
			default:
				outputs++
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				c, err := Parse([]byte(sec.text))
				if err != nil {
					t.Fatalf("Parse: %v", err)
				}
				got := c.Print()
				if sec.output && got != sec.text {
					t.Fatalf("Print(Parse(x)) != x\n--- got\n%s\n--- want\n%s", got, sec.text)
				}
				assertFixedPoint(t, got)
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if transformOutputs != 7 {
		t.Errorf("%d transforms outputs round-tripped, want all 7", transformOutputs)
	}
	if outputs < 30 {
		t.Errorf("only %d outputs round-tripped, the testdata is incomplete", outputs)
	}
	t.Logf("%d outputs (%d from transforms), %d version 5 inputs; skipped: %v", outputs, transformOutputs, inputs, skipped)
	for _, s := range skipped {
		if !strings.Contains(s, "#0 ") {
			t.Errorf("unexpected skipped lockfile %s", s)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// assertFixedPoint checks that printing the parse of canonical text gives the same text.
func assertFixedPoint(t *testing.T, canonical string) {
	t.Helper()
	c, err := Parse([]byte(canonical))
	if err != nil {
		t.Fatalf("Parse of the printed text: %v\n%s", err, canonical)
	}
	if again := c.Print(); again != canonical {
		t.Fatalf("not a fixed point\n--- first\n%s\n--- second\n%s", canonical, again)
	}
}

// TestRoundTripKnopeFixtures round-trips the deno.lock files of Knope's prepare_release/deno fixtures.
func TestRoundTripKnopeFixtures(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("testdata/knope/*/*/deno.lock")
	if err != nil || len(files) != 4 {
		t.Fatalf("fixtures: %v, %v", files, err)
	}
	for _, path := range files {
		t.Run(filepath.ToSlash(path), func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			c, err := Parse(data)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			got := c.Print()
			if strings.Contains(path, "/out/") {
				if got != string(data) {
					t.Fatalf("Print(Parse(x)) != x\n--- got\n%s\n--- want\n%s", got, data)
				}
				return
			}
			// the inputs are Knope's hand-written samples: empty sections go and requirements are normalised
			assertFixedPoint(t, got)
		})
	}
}
