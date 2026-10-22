package versionedfile_test

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	pkgLockPath      = "pkg/package-lock.json"
	pkgLockWarning   = "package-lock.json lockfileVersion is not 2 or 3, errors may occur"
	pkgLockDeserCode = "remeris::package_lock_json::deserialize"
	pkgLockMissing   = pkgLockPath + " did not have a `version` field"
	pkgLockMissCode  = "remeris::package_lock_json::missing_version"
	pkgLockMissHelp  = "Either add a `version` field or specify a `dependency` to update"
	pkgLockInvalCode = "remeris::package_lock_json::invalid_version"
	pkgLockInvalHelp = "Only plain version numbers are currently supported, e.g. `1.2.3`"
	pkgLockDepName   = "dependency-name"
	pkgLockDepDiff   = "dependency-name = 2.1.0"
	pkgLockNoMatch   = "{\n  \"version\": \"1.0.0\",\n  \"packages\": {\n    \"\": {\n      \"version\": \"1.0.0\"\n    }\n  }\n}"
)

const pkgLockVersionContent = `{
        "name": "tester",
        "version": "0.1.0-rc.0"
        }`

const pkgLockSetContent = `{
        "name": "tester",
        "version": "0.1.0-rc.0",
        "packages": {
          "": {
            "version": "0.1.0-rc.0"
          }
        }
        }`

const pkgLockDepsContent = `{
    "name": "tester",
    "version": "1.0.0",
    "packages": {
        "": {
            "dependencies": {"dependency-name": "2.0.0", "some-other-dependency": "0.1.0"},
            "devDependencies": {"@another/dev-dependency": "0.2.0", "@dev/dependency-name": "3.0.0"}
        },
        "a": {
            "name": "dependency-name",
            "version": "2.0.0",
            "dependencies": {"some-other-dependency": "0.1.0"},
            "devDependencies": {"@another/dev-dependency": "0.2.0", "@dev/dependency-name": "3.0.0"}
        },
        "b": {
            "name": "some-other-dependency",
            "version": "0.1.0",
            "dependencies": {"dependency-name": "2.0.0"},
            "devDependencies": {"@another/dev-dependency": "0.2.0", "@dev/dependency-name": "3.0.0"}
        },
        "c": {
            "name": "@another/dev-dependency",
            "version": "0.2.0",
            "dependencies": {"dependency-name": "2.0.0"},
            "devDependencies": {"@dev/dependency-name": "3.0.0"}
        },
        "d": {
            "name": "@dev/dependency-name",
            "version": "3.0.0",
            "dependencies": {"dependency-name": "2.0.0"},
            "devDependencies": {"@another/dev-dependency": "0.2.0"}
        }
    }
}`

// pkgLockDepsPretty is pkgLockDepsContent printed like serde_json to_string_pretty.
const pkgLockDepsPretty = `{
  "name": "tester",
  "version": "1.0.0",
  "packages": {
    "": {
      "dependencies": {
        "dependency-name": "2.0.0",
        "some-other-dependency": "0.1.0"
      },
      "devDependencies": {
        "@another/dev-dependency": "0.2.0",
        "@dev/dependency-name": "3.0.0"
      }
    },
    "a": {
      "name": "dependency-name",
      "version": "2.0.0",
      "dependencies": {
        "some-other-dependency": "0.1.0"
      },
      "devDependencies": {
        "@another/dev-dependency": "0.2.0",
        "@dev/dependency-name": "3.0.0"
      }
    },
    "b": {
      "name": "some-other-dependency",
      "version": "0.1.0",
      "dependencies": {
        "dependency-name": "2.0.0"
      },
      "devDependencies": {
        "@another/dev-dependency": "0.2.0",
        "@dev/dependency-name": "3.0.0"
      }
    },
    "c": {
      "name": "@another/dev-dependency",
      "version": "0.2.0",
      "dependencies": {
        "dependency-name": "2.0.0"
      },
      "devDependencies": {
        "@dev/dependency-name": "3.0.0"
      }
    },
    "d": {
      "name": "@dev/dependency-name",
      "version": "3.0.0",
      "dependencies": {
        "dependency-name": "2.0.0"
      },
      "devDependencies": {
        "@another/dev-dependency": "0.2.0"
      }
    }
  }
}`

// Documents shared by the JSON versioned file tests (package-lock.json and tauri.conf.json). They are constants because
// goconst counts literals across the whole package.
const (
	jsonDuplicateVersion = `{"version": "1.0.0", "version": "2.0.0"}`
	jsonNumberVersion    = `{"version": 1}`
	jsonNullVersion      = `{"version": null}`
	jsonWordVersion      = `{"version": "one"}`
	jsonTrailingComma    = `{"version": "1.0.0",}`
	jsonTrailingText     = `{"version": "1.0.0"} x`
	jsonCompactOne       = `{"version":"1.0.0"}`
	jsonOneNewline       = "{\"version\": \"1.0.0\"}\n"
	prettyV110           = "{\n  \"version\": \"1.1.0\"\n}"
	caseArray            = "array root"
)

func newPkgLock(content string) (versionedfile.File, error) {
	return versionedfile.New(versionedfile.Config{Path: pkgLockPath, Format: versionedfile.PackageLockJson}, content, nil)
}

func mustPkgLock(t *testing.T, content string) versionedfile.File {
	t.Helper()
	f, err := newPkgLock(content)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// capturePkgLockLog replaces the default logger and returns what it logged. The logger is global, so callers must not
// run in parallel.
func capturePkgLockLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(
		slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey || a.Key == slog.LevelKey {
				return slog.Attr{}
			}
			return a
		}})),
	)
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

func TestPackageLockVersion(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		content string
		want    string
	}{
		"lock plain":          {pkgLockVersionContent, npmRC0},
		"lock duplicate wins": {jsonDuplicateVersion, npmV200},
		"with packages":       {pkgLockSetContent, npmRC0},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := mustPkgLock(t, tc.content)
			got, err := f.Version()
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != tc.want || f.Path() != pkgLockPath {
				t.Errorf("got %s at %s, want %s", got, f.Path(), tc.want)
			}
		})
	}
}

func TestPackageLockVersionErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		content string
		message string
		code    string
		help    string
	}{
		"lock missing":   {`{"name": "x"}`, pkgLockMissing, pkgLockMissCode, pkgLockMissHelp},
		"number version": {jsonNumberVersion, pkgLockMissing, pkgLockMissCode, pkgLockMissHelp},
		"null":           {jsonNullVersion, pkgLockMissing, pkgLockMissCode, pkgLockMissHelp},
		caseArray:        {`["2.0.0"]`, pkgLockMissing, pkgLockMissCode, pkgLockMissHelp},
		"not a version":  {jsonWordVersion, "Error parsing " + pkgLockPath + ": ", pkgLockInvalCode, pkgLockInvalHelp},
		"range":          {`{"version": "^1.0.0"}`, "Error parsing " + pkgLockPath + ": ", pkgLockInvalCode, pkgLockInvalHelp},
		"empty version":  {`{"version": ""}`, "Error parsing " + pkgLockPath + ": ", pkgLockInvalCode, pkgLockInvalHelp},
		"v prefix":       {`{"version": "v1.0.0"}`, "Error parsing " + pkgLockPath + ": ", pkgLockInvalCode, pkgLockInvalHelp},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Parsing the file succeeds; the version is read on demand, so a dependency edit still works.
			f := mustPkgLock(t, tc.content)
			_, err := f.Version()
			if err == nil || !strings.HasPrefix(err.Error(), tc.message) {
				t.Fatalf("got %v, want message starting %q", err, tc.message)
			}
			var d diag.Diagnostic
			if !errors.As(err, &d) {
				t.Fatalf("%T is not a diagnostic", err)
			}
			if d.Code() != tc.code || d.Help() != tc.help {
				t.Errorf("code %q, help %q", d.Code(), d.Help())
			}
		})
	}
}

func TestPackageLockInvalidJSON(t *testing.T) {
	t.Parallel()
	prefix := "Error deserializing " + pkgLockPath + ": "
	tests := map[string]string{
		"lock empty":         ``,
		"lock syntax":        jsonTrailingComma,
		"lock trailing text": jsonTrailingText,
		"truncated":          `{"version": `,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, err := newPkgLock(content)
			if f != nil || err == nil || !strings.HasPrefix(err.Error(), prefix) {
				t.Fatalf("got %v, %v", f, err)
			}
			var d diag.Diagnostic
			if !errors.As(err, &d) || d.Code() != pkgLockDeserCode || d.Help() != "" {
				t.Errorf("code or help wrong: %#v", err)
			}
		})
	}
}

func TestPackageLockSetVersion(t *testing.T) {
	t.Parallel()
	dep := strp(pkgLockDepName)
	tests := map[string]struct {
		content string
		steps   []cargoStep
		want    string // "" means no write
		diff    string
	}{
		"lock set version": {
			content: pkgLockSetContent,
			steps:   []cargoStep{{npmV123rc4, nil}},
			want: "{\n  \"name\": \"tester\",\n  \"version\": \"1.2.3-rc.4\",\n  \"packages\": {\n    \"\": {\n" +
				"      \"version\": \"1.2.3-rc.4\"\n    }\n  }\n}",
			diff: npmDiffV,
		},
		"without packages": {
			content: pkgLockVersionContent,
			steps:   []cargoStep{{npmV123rc4, nil}},
			want:    "{\n  \"name\": \"tester\",\n  \"version\": \"1.2.3-rc.4\"\n}",
			diff:    npmDiffV,
		},
		"root package without version gains one": {
			content: `{"version":"1.0.0","packages":{"":{"name":"x"}}}`,
			steps:   []cargoStep{{v110, nil}},
			want:    "{\n  \"version\": \"1.1.0\",\n  \"packages\": {\n    \"\": {\n      \"name\": \"x\",\n      \"version\": \"1.1.0\"\n    }\n  }\n}",
			diff:    npmDiffV110,
		},
		"other packages are untouched": {
			content: `{"version":"1.0.0","packages":{"":{"version":"1.0.0"},"node_modules/a":{"version":"1.0.0"}}}`,
			steps:   []cargoStep{{v110, nil}},
			want: "{\n  \"version\": \"1.1.0\",\n  \"packages\": {\n    \"\": {\n      \"version\": \"1.1.0\"\n    },\n" +
				"    \"node_modules/a\": {\n      \"version\": \"1.0.0\"\n    }\n  }\n}",
			diff: npmDiffV110,
		},
		"packages is not an object": {
			content: `{"version":"1.0.0","packages":["x"]}`,
			steps:   []cargoStep{{v110, nil}},
			want:    "{\n  \"version\": \"1.1.0\",\n  \"packages\": [\n    \"x\"\n  ]\n}",
			diff:    npmDiffV110,
		},
		"root package is not an object": {
			content: `{"version":"1.0.0","packages":{"":"x"}}`,
			steps:   []cargoStep{{v110, nil}},
			want:    "{\n  \"version\": \"1.1.0\",\n  \"packages\": {\n    \"\": \"x\"\n  }\n}",
			diff:    npmDiffV110,
		},
		"lock same version still writes": {
			content: jsonCompactOne,
			steps:   []cargoStep{{pyOne, nil}},
			want:    "{\n  \"version\": \"" + pyOne + "\"\n}",
			diff:    "version = " + pyOne,
		},
		"lock trailing newline is dropped": {
			content: jsonOneNewline,
			steps:   []cargoStep{{v110, nil}},
			want:    prettyV110,
			diff:    npmDiffV110,
		},
		"duplicate keys keep the last value": {
			content: `{"version":"1.0.0","a":1,"version":"1.0.1","a":2}`,
			steps:   []cargoStep{{v110, nil}},
			want:    "{\n  \"version\": \"1.1.0\",\n  \"a\": 2\n}",
			diff:    npmDiffV110,
		},
		"dependency regular": {
			content: pkgLockDepsContent,
			steps:   []cargoStep{{npmV210, dep}},
			want:    strings.ReplaceAll(pkgLockDepsPretty, "2.0.0", npmV210),
			diff:    pkgLockDepDiff,
		},
		"dependency dev": {
			content: pkgLockDepsContent,
			steps:   []cargoStep{{"3.1.0", strp(npmDevDep)}},
			want:    strings.ReplaceAll(pkgLockDepsPretty, "3.0.0", "3.1.0"),
			diff:    "@dev/dependency-name = 3.1.0",
		},
		"dependency without a match still writes": {
			content: pkgLockSetContent,
			steps:   []cargoStep{{npmV210, dep}},
			want: "{\n  \"name\": \"tester\",\n  \"version\": \"0.1.0-rc.0\",\n  \"packages\": {\n    \"\": {\n" +
				"      \"version\": \"0.1.0-rc.0\"\n    }\n  }\n}",
			diff: pkgLockDepDiff,
		},
		"dependency without packages still writes": {
			content: `{"version":"1.0.0","lockfileVersion":3}`,
			steps:   []cargoStep{{npmV210, dep}},
			want:    "{\n  \"version\": \"1.0.0\",\n  \"lockfileVersion\": 3\n}",
			diff:    pkgLockDepDiff,
		},
		"dependency with packages that is not an object": {
			content: `{"packages":[]}`,
			steps:   []cargoStep{{npmV210, dep}},
			want:    "{\n  \"packages\": []\n}",
			diff:    pkgLockDepDiff,
		},
		"matching by the name field only": {
			content: `{"version":"1.0.0","packages":{"node_modules/dependency-name":{"version":"2.0.0"}}}`,
			steps:   []cargoStep{{npmV210, dep}},
			want:    "{\n  \"version\": \"1.0.0\",\n  \"packages\": {\n    \"node_modules/dependency-name\": {\n      \"version\": \"2.0.0\"\n    }\n  }\n}",
			diff:    pkgLockDepDiff,
		},
		"the legacy dependencies section is untouched": {
			content: `{"version":"1.0.0","dependencies":{"dependency-name":{"version":"2.0.0"}}}`,
			steps:   []cargoStep{{npmV210, dep}},
			want:    "{\n  \"version\": \"1.0.0\",\n  \"dependencies\": {\n    \"dependency-name\": {\n      \"version\": \"2.0.0\"\n    }\n  }\n}",
			diff:    pkgLockDepDiff,
		},
		"name field matches and the caret is lost": {
			content: `{"packages":{"x":{"name":"dependency-name","version":"^2.0.0","dependencies":{"dependency-name":"^2.0.0"}}}}`,
			steps:   []cargoStep{{npmV210, dep}},
			want: "{\n  \"packages\": {\n    \"x\": {\n      \"name\": \"dependency-name\",\n      \"version\": \"2.1.0\",\n" +
				"      \"dependencies\": {\n        \"dependency-name\": \"2.1.0\"\n      }\n    }\n  }\n}",
			diff: pkgLockDepDiff,
		},
		"a matching entry without version gains one at the end": {
			content: `{"packages":{"x":{"name":"dependency-name","license":"MIT"}}}`,
			steps:   []cargoStep{{npmV210, dep}},
			want:    "{\n  \"packages\": {\n    \"x\": {\n      \"name\": \"dependency-name\",\n      \"license\": \"MIT\",\n      \"version\": \"2.1.0\"\n    }\n  }\n}",
			diff:    pkgLockDepDiff,
		},
		"a dependency value of any type is replaced": {
			content: `{"packages":{"x":{"dependencies":{"dependency-name":{"v":1}},"devDependencies":{"dependency-name":null}}}}`,
			steps:   []cargoStep{{npmV210, dep}},
			want: "{\n  \"packages\": {\n    \"x\": {\n      \"dependencies\": {\n        \"dependency-name\": \"2.1.0\"\n      },\n" +
				"      \"devDependencies\": {\n        \"dependency-name\": \"2.1.0\"\n      }\n    }\n  }\n}",
			diff: pkgLockDepDiff,
		},
		"non-object entries and a non-string name are skipped": {
			content: `{"packages":{"a":1,"b":null,"c":{"name":5,"version":"1.0.0"},"d":{"dependencies":[1]}}}`,
			steps:   []cargoStep{{npmV210, dep}},
			want: "{\n  \"packages\": {\n    \"a\": 1,\n    \"b\": null,\n    \"c\": {\n      \"name\": 5,\n      \"version\": \"1.0.0\"\n    },\n" +
				"    \"d\": {\n      \"dependencies\": [\n        1\n      ]\n    }\n  }\n}",
			diff: pkgLockDepDiff,
		},
		"version then dependency": {
			content: pkgLockNoMatch,
			steps:   []cargoStep{{npmV150, nil}, {npmV150, dep}},
			want:    strings.ReplaceAll(pkgLockNoMatch, "1.0.0", npmV150),
			diff:    "version = 1.5.0, dependency-name = 1.5.0",
		},
		"two dependencies": {
			content: `{"packages":{"x":{"dependencies":{"a":"1.0.0","b":"1.0.0"}}}}`,
			steps:   []cargoStep{{v110, strp("a")}, {"1.2.5", strp("b")}},
			want:    "{\n  \"packages\": {\n    \"x\": {\n      \"dependencies\": {\n        \"a\": \"1.1.0\",\n        \"b\": \"1.2.5\"\n      }\n    }\n  }\n}",
			diff:    "a = 1.1.0, b = 1.2.5",
		},
		"lock numbers and unicode survive": {
			content: `{"version":"1.0.0","n":[1,1.0,1e3,-0,18446744073709551615],"s":"héllo 世 </>\u0001"}`,
			steps:   []cargoStep{{goV101, nil}},
			want: "{\n  \"version\": \"1.0.1\",\n  \"n\": [\n    1,\n    1.0,\n    1000.0,\n    -0.0,\n    18446744073709551615\n  ],\n" +
				"  \"s\": \"héllo 世 </>\\u0001\"\n}",
			diff: "version = " + goV101,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := applySteps(t, mustPkgLock(t, tc.content), tc.steps).Write()
			if tc.want == "" {
				if got != nil {
					t.Fatalf("expected no write, got %v", got)
				}
				return
			}
			want := action.WriteToFile(pkgLockPath, tc.want, tc.diff)
			if len(got) != 1 || !got[0].Equal(want) {
				t.Errorf("got %#v\nwant %#v", got, want)
			}
		})
	}
}

func TestPackageLockNoEditNoWrite(t *testing.T) {
	t.Parallel()
	if got := mustPkgLock(t, pkgLockSetContent).Write(); got != nil {
		t.Errorf("got %v", got)
	}
}

func TestPackageLockIdempotent(t *testing.T) {
	t.Parallel()
	v := mustVersion(t, npmV123rc4)
	first, err := mustPkgLock(t, pkgLockSetContent).SetVersion(v, nil, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	content := first.Write()[0].Content
	again := mustPkgLock(t, content)
	second, err := again.SetVersion(v, nil, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	if got := second.Write()[0].Content; got != content {
		t.Errorf("second edit changed the text:\n%s\nvs\n%s", got, content)
	}
}

func TestPackageLockImmutable(t *testing.T) {
	t.Parallel()
	orig := mustPkgLock(t, pkgLockSetContent)
	edited, err := orig.SetVersion(mustVersion(t, npmV200), nil, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := edited.SetVersion(mustVersion(t, npmV300), strp(pkgLockDepName), versionedfile.Standard); err != nil {
		t.Fatal(err)
	}
	if orig.Write() != nil {
		t.Error("the original gained a write")
	}
	got := edited.Write()
	if len(got) != 1 || got[0].Diff != "version = 2.0.0" {
		t.Errorf("edited changed after a later SetVersion: %#v", got)
	}
}

// TestPackageLockWarnings replaces the default logger, so it must not run in parallel.
func TestPackageLockWarnings(t *testing.T) {
	warning := "msg=\"" + pkgLockWarning + "\"\n"
	tests := map[string]struct {
		content string
		want    string
	}{
		"2":                       {`{"version":"1.0.0","lockfileVersion":2}`, ""},
		"3":                       {`{"version":"1.0.0","lockfileVersion":3}`, ""},
		"1":                       {`{"version":"1.0.0","lockfileVersion":1}`, warning},
		"4":                       {`{"version":"1.0.0","lockfileVersion":4}`, warning},
		"lock missing":            {jsonCompactOne, warning},
		"3.0":                     {`{"version":"1.0.0","lockfileVersion":3.0}`, warning},
		"2.5":                     {`{"version":"1.0.0","lockfileVersion":2.5}`, warning},
		"lockfileVersion as text": {`{"version":"1.0.0","lockfileVersion":"3"}`, warning},
		"null":                    {`{"version":"1.0.0","lockfileVersion":null}`, warning},
		"huge":                    {`{"version":"1.0.0","lockfileVersion":18446744073709551615}`, warning},
		"3e0 is a float":          {`{"version":"1.0.0","lockfileVersion":3e0}`, warning},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			buf := capturePkgLockLog(t)
			f := mustPkgLock(t, tc.content)
			if buf.Len() != 0 {
				t.Fatalf("parsing warned: %q", buf.String())
			}
			if _, err := f.SetVersion(mustVersion(t, v110), nil, versionedfile.Standard); err != nil {
				t.Fatal(err)
			}
			if got := buf.String(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPackageLockWarnsOnEveryCall(t *testing.T) {
	buf := capturePkgLockLog(t)
	f := mustPkgLock(t, jsonCompactOne)
	for _, dep := range []*string{nil, strp("a"), strp("b")} {
		var err error
		if f, err = f.SetVersion(mustVersion(t, v110), dep, versionedfile.Standard); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := strings.Count(buf.String(), pkgLockWarning), 3; got != want {
		t.Errorf("warned %d times, want %d: %q", got, want, buf.String())
	}
	if _, err := f.Version(); err != nil || strings.Count(buf.String(), pkgLockWarning) != 3 {
		t.Errorf("Version warned or failed: %v", err)
	}
}

func FuzzPackageLockSetVersion(f *testing.F) {
	for _, seed := range []string{
		pkgLockSetContent, pkgLockDepsContent, pkgLockNoMatch, `{}`, `[]`, `{"packages":{"":{"name":"a","dependencies":{"a":1}}}}`,
		`{"version":"1.0.0","lockfileVersion":3.0}`, "\ufeff{}", `{"version":"1.0.0","version":"2.0.0"}`,
	} {
		f.Add(seed, true)
		f.Add(seed, false)
	}
	quiet := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	f.Fuzz(func(t *testing.T, content string, withDependency bool) {
		old := slog.Default()
		slog.SetDefault(quiet)
		defer slog.SetDefault(old)
		file, err := newPkgLock(content)
		if err != nil {
			return
		}
		var dep *string
		if withDependency {
			dep = strp("a")
		}
		edited, err := file.SetVersion(mustVersion(t, v110), dep, versionedfile.Standard)
		if err != nil {
			t.Fatalf("SetVersion failed on a parsed file: %v", err)
		}
		written := edited.Write()
		if len(written) != 1 {
			t.Fatalf("got %d writes", len(written))
		}
		// Not idempotence: like serde_json without float_roundtrip, a number with a large exponent can print as
		// another float than it was read as, and printing it again then changes it.
		if _, err := newPkgLock(written[0].Content); err != nil {
			t.Fatalf("output does not parse: %v\n%s", err, written[0].Content)
		}
	})
}
