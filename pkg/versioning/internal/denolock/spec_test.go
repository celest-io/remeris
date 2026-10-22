package denolock

import (
	"io/fs"
	"os"
	"path/filepath"
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

// lockfileSections returns the sections of a spec file that hold a lockfile: the first ("original"), then every
// second one after it (each "change" is followed by its "output").
func lockfileSections(t *testing.T, text string) []string {
	t.Helper()
	sections, ok := splitSections(text)
	if !ok {
		t.Fatal("expected a section header")
	}
	var out []string
	for i := 0; i < len(sections); i += 2 {
		out = append(out, sections[i])
	}
	return out
}

func isV5(t *testing.T, text string) bool {
	t.Helper()
	root, err := ParseJSON([]byte(text))
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	v, ok := Version(root)
	return ok && v == "5"
}

// TestRoundTripSpecs reads every version 5 lockfile of the deno_lockfile spec files and prints it again. The outputs of
// the specs are what Deno's printer wrote, so they must come back byte for byte; the inputs were written by hand and
// only need to be a fixed point after one round.
func TestRoundTripSpecs(t *testing.T) {
	t.Parallel()
	count := 0
	fsys := os.DirFS("testdata/specs")
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".txt" {
			return err
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		for i, text := range lockfileSections(t, string(data)) {
			if !isV5(t, text) {
				continue
			}
			count++
			name := path + "#" + string(rune('0'+i))
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				c, err := Parse([]byte(text))
				if err != nil {
					t.Fatalf("Parse: %v", err)
				}
				got := c.Print()
				if i > 0 && got != text { // the outputs: not the original
					t.Fatalf("Print(Parse(x)) != x\n--- got\n%s\n--- want\n%s", got, text)
				}
				assertFixedPoint(t, got)
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count < 30 {
		t.Fatalf("only %d version 5 lockfiles found in the specs, the testdata is incomplete", count)
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
