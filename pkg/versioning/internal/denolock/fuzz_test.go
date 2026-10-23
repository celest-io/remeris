package denolock

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// FuzzParsePrint checks that reading arbitrary bytes never panics, and that whatever reads can be printed and that
// the printed text reads back to a text that does not change any more.
func FuzzParsePrint(f *testing.F) {
	fsys := os.DirFS("testdata")
	_ = fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // a seed that cannot be read is skipped
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return nil //nolint:nilerr // a seed that cannot be read is skipped
		}
		if filepath.Ext(path) == ".txt" {
			// a spec file: seed with each of its sections
			sections, _ := splitSections(string(data))
			for _, section := range sections {
				f.Add([]byte(section))
			}
			return nil
		}
		f.Add(data)
		return nil
	})
	for _, seed := range []string{
		``, emptyObject, emptyArray, jsonNull, `{"specifiers":{"jsr:a":"1"},"jsr":{"a@1.0.0":{"integrity":"x","dependencies":["jsr:a"]}}}`,
		`{"npm":{"a@1.0.0":{"dependencies":["a"],"os":["x"]},"b@npm:a@1":{}}}`,
		`{"workspace":{"packageJson":{"overrides":{"os":[1,{"cpu":[2]}]}},"links":{"jsr:a@1":{"peerDependenciesMeta":{"x":1}}}}}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := Parse(data)
		if err != nil {
			return
		}
		first := c.Print()
		again, err := Parse([]byte(first))
		if err != nil {
			t.Fatalf("the printed text does not read back: %v\n%s", err, first)
		}
		second := again.Print()
		if second != first {
			t.Fatalf("not idempotent\n--- first\n%s\n--- second\n%s", first, second)
		}
		third, err := Parse([]byte(second))
		if err != nil {
			t.Fatalf("the second printed text does not read back: %v\n%s", err, second)
		}
		if got := third.Print(); got != second {
			t.Fatalf("not a fixed point\n--- second\n%s\n--- third\n%s", second, got)
		}
	})
}
