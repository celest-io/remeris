package denosemver_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tsvRows reads a fixture file of testdata: one row per line, fields separated by a tab, "\t" and "\n" in a field
// standing for a tab and a newline. Empty lines and lines that start with "#" are skipped. The name of a row is
// its line, for the name of the subtest.
func tsvRows(t testing.TB, name string) []row {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	unescape := strings.NewReplacer(`\t`, "\t", `\n`, "\n")
	var rows []row
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSuffix(line, "\n")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		for i, f := range fields {
			fields[i] = unescape.Replace(f)
		}
		rows = append(rows, row{name: line, fields: fields})
	}
	if len(rows) == 0 {
		t.Fatalf("no rows in %s", name)
	}
	return rows
}

// The names of the grammars in the first column of the fixtures.
const (
	grammarStrict     = "strict"
	grammarLoose      = "loose"
	grammarSpecifier  = "specifier"
	grammarNormalized = "normalized"
)

type row struct {
	name   string
	fields []string
}

// field returns the i-th field, failing the test when the row is too short.
func (r row) field(t *testing.T, i int) string {
	t.Helper()
	if i >= len(r.fields) {
		t.Fatalf("row %q has %d fields, want at least %d", r.name, len(r.fields), i+1)
	}
	return r.fields[i]
}

// sign is the sign of a comparison, as the fixtures write it.
func sign(t *testing.T, s string) int {
	t.Helper()
	switch s {
	case "-1":
		return -1
	case "0":
		return 0
	case "1":
		return 1
	}
	t.Fatalf("bad sign %q", s)
	return 0
}
