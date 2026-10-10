package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/celest-io/remeris/internal/steps"
)

// scriptDir holds one directory per suite of golden scripts. Fixtures ported from Knope keep the
// name of their Knope suite (see testdata/README.md).
const scriptDir = "../../testdata/script"

// fakeDateEnv fixes the date of a release in the scripts. Only this test hook reads it (spec, testing strategy).
const fakeDateEnv = "REMERIS_FAKE_DATE"

// TestMain registers the binary as the `remeris` command of the scripts, so no build step is
// needed and the code under test is the code in this module.
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){"remeris": func() {
		if date := os.Getenv(fakeDateEnv); date != "" {
			steps.Today = func() string { return date }
		}
		main()
	}})
}

func TestScripts(t *testing.T) {
	suites, err := os.ReadDir(scriptDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, suite := range suites {
		if !suite.IsDir() {
			continue
		}
		t.Run(suite.Name(), func(t *testing.T) {
			testscript.Run(t, testscript.Params{
				Dir:           filepath.Join(scriptDir, suite.Name()),
				Cmds:          map[string]func(*testscript.TestScript, bool, []string){"rstrip": rstrip},
				UpdateScripts: os.Getenv("UPDATE_SCRIPTS") != "",
			})
		})
	}
}

// rstrip writes the file named by its first argument to the second one without the spaces at the end of its
// lines. Some expected outputs end a line with a space ("Would add the following to CHANGELOG.md: "), which the
// whitespace check of the repository does not accept in a script, so the output is compared without them.
// stdout and stderr can be the source.
func rstrip(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 2 {
		ts.Fatalf("usage: rstrip src dst")
	}
	lines := strings.Split(ts.ReadFile(args[0]), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	if err := os.WriteFile(ts.MkAbs(args[1]), []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		ts.Fatalf("rstrip: %v", err)
	}
}
