package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

// scriptDir holds one directory per suite of golden scripts. Fixtures ported from Knope keep the
// name of their Knope suite (see testdata/README.md).
const scriptDir = "../../testdata/script"

// TestMain registers the binary as the `remeris` command of the scripts, so no build step is
// needed and the code under test is the code in this module.
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){"remeris": main})
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
				UpdateScripts: os.Getenv("UPDATE_SCRIPTS") != "",
			})
		})
	}
}
