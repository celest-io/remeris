package versionedfile_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/internal/denolock"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	denoLockMemberFirst = "first"
	denoLockV011        = "0.1.1"
)

const (
	denoLockPath  = "deno.lock"
	denoFirst     = "@scope/first"
	denoLeftPad   = "left-pad"
	denoVersion5  = `"version": "5"`
	denoJSRFirst  = "jsr:@scope/first@1.2.0"
	denoNPMLeft14 = "npm:left-pad@1.4.0"
	denoLockV120  = "1.2.0"
	denoRedirects = `"redirects": 5`

	denoWorkspace    = "workspace"
	denoSA1          = "@s/a@1.0.0"
	denoSA           = "@s/a"
	denoJSRA2        = "jsr:@s/a@2.0.0"
	denoJSRA1        = "jsr:@s/a@1"
	denoV140         = "1.4.0"
	denoDeps         = "dependencies"
	denoRC1          = "1.0.0-rc.1"
	denoStarKey      = "jsr:@s/a@*"
	denoLockEmptyObj = "{}"
	denoLockEmptyArr = "[]"
	denoDeserPrefix  = "Error deserializing deno.lock: "
	denoLegacy1      = "Lockfile version 1 in deno.lock requires npm package metadata which is not supported yet"
)

// denoSample is the lockfile of the unit tests of Knope's deno_lock.rs.
const denoSample = `{
  "version": "5",
  "specifiers": {
    "jsr:@scope/first@^1.0.0": "1.0.0",
    "npm:left-pad@^1": "1.3.0"
  },
  "jsr": {
    "@scope/first@1.0.0": {
      "integrity": "sha256-abc",
      "dependencies": []
    }
  },
  "npm": {
    "left-pad@1.3.0": {
      "integrity": "sha256-def",
      "dependencies": []
    }
  },
  "redirects": {},
  "remote": {},
  "workspace": {
    "dependencies": [
      "jsr:@scope/first@1.0.0",
      "npm:left-pad@1.3.0"
    ],
    "packageJson": {
      "dependencies": [
        "jsr:@scope/first@^1.0.0",
        "npm:left-pad@^1"
      ]
    },
    "members": {
      "first": {
        "dependencies": [
          "jsr:@scope/first@1.0.0",
          "npm:left-pad@1.3.0"
        ],
        "packageJson": {
          "dependencies": [
            "jsr:@scope/first@^1.0.0",
            "npm:left-pad@^1"
          ]
        }
      }
    },
    "links": {
      "jsr:@scope/first@1.0.0": {
        "dependencies": [],
        "optionalDependencies": [],
        "peerDependencies": [],
        "peerDependenciesMeta": {}
      },
      "npm:left-pad@1.3.0": {
        "dependencies": [],
        "optionalDependencies": [
          "npm:left-pad@1.3.0"
        ],
        "peerDependencies": [
          "npm:left-pad@^1"
        ],
        "peerDependenciesMeta": {}
      }
    }
  }
}`

func newDenoLock(content string) (versionedfile.File, error) {
	return versionedfile.New(versionedfile.Config{Path: denoLockPath, Format: versionedfile.DenoLock}, content, nil)
}

// setDeno applies one SetVersion per step and returns the file.
func setDeno(t *testing.T, content string, steps ...[2]string) versionedfile.File {
	t.Helper()
	f, err := newDenoLock(content)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range steps {
		if f, err = f.SetVersion(mustVersion(t, s[1]), strp(s[0]), versionedfile.Standard); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

// jsonAt decodes text and follows keys through objects.
func jsonAt(t *testing.T, text string, keys ...string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, text)
	}
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("%v is not an object on the way to %v", v, keys)
		}
		if v, ok = m[k]; !ok {
			return nil
		}
	}
	return v
}

func strs(items ...string) []any {
	out := make([]any, len(items))
	for i, s := range items {
		out[i] = s
	}
	return out
}

func TestDenoLockRustUnitTests(t *testing.T) {
	t.Parallel()
	t.Run("updates specifiers and workspace", func(t *testing.T) {
		t.Parallel()
		content, diff := lockWrite(t, setDeno(t, denoSample, [2]string{denoFirst, denoLockV120}))
		for _, want := range []string{`"jsr:@scope/first@1": "1.2.0"`, `"@scope/first@1.2.0"`, `"` + denoJSRFirst + `"`} {
			if !strings.Contains(content, want) {
				t.Errorf("output lacks %s:\n%s", want, content)
			}
		}
		if diff != denoFirst+" = "+denoLockV120 {
			t.Errorf("diff %q", diff)
		}
	})
	t.Run("ignores a missing dependency", func(t *testing.T) {
		t.Parallel()
		if acts := setDeno(t, denoSample, [2]string{"@scope/second", denoLockV120}).Write(); acts != nil {
			t.Errorf("got %v, want no action", acts)
		}
	})
	t.Run("updates an npm dependency", func(t *testing.T) {
		t.Parallel()
		content, diff := lockWrite(t, setDeno(t, denoSample, [2]string{denoLeftPad, denoV140}))
		checks := []struct {
			path []string
			want any
		}{
			{[]string{"specifiers", "npm:left-pad@1"}, denoV140},
			{[]string{denoWorkspace, denoDeps}, strs("jsr:@scope/first@1.0.0", denoNPMLeft14)},
			{[]string{denoWorkspace, "packageJson", denoDeps}, strs("jsr:@scope/first@1", "npm:left-pad@1.4.0")},
			{[]string{denoWorkspace, "members", denoLockMemberFirst, denoDeps}, strs("jsr:@scope/first@1.0.0", denoNPMLeft14)},
			{
				[]string{denoWorkspace, "members", "first", "packageJson", denoDeps},
				strs("jsr:@scope/first@1", denoNPMLeft14),
			},
			{[]string{denoWorkspace, "links", denoNPMLeft14, "optionalDependencies"}, strs(denoNPMLeft14)},
			{[]string{denoWorkspace, "links", denoNPMLeft14, "peerDependencies"}, strs(denoNPMLeft14)},
		}
		for _, c := range checks {
			if got := jsonAt(t, content, c.path...); !reflect.DeepEqual(got, c.want) {
				t.Errorf("%v = %v, want %v\n%s", c.path, got, c.want, content)
			}
		}
		if jsonAt(t, content, "npm", "left-pad@1.4.0") == nil || jsonAt(t, content, "npm", "left-pad@1.3.0") != nil {
			t.Errorf("npm section not renamed:\n%s", content)
		}
		if diff != "left-pad = 1.4.0" {
			t.Errorf("diff %q", diff)
		}
	})
	t.Run("legacy version errors", func(t *testing.T) {
		t.Parallel()
		if _, err := newDenoLock(`{"version":"4"}`); err == nil {
			t.Error("want an error")
		}
	})
}

func TestDenoLockLoad(t *testing.T) {
	t.Parallel()
	const (
		legacyCode = "remeris::deno_lock::legacy_version"
		unsupCode  = "remeris::deno_lock::unsupported_version"
		deserCode  = "remeris::deno_lock::deserialize"
	)
	tests := map[string]struct {
		content  string
		wantMsg  string
		wantCode string
	}{
		"load v1": {
			`{"version":"1"}`,
			denoLegacy1,
			legacyCode,
		},
		"load v2": {
			`{"version":"2"}`,
			"Lockfile version 2 in deno.lock requires npm package metadata which is not supported yet",
			legacyCode,
		},
		"load v3": {
			`{"version":"3"}`,
			"Lockfile version 3 in deno.lock requires npm package metadata which is not supported yet",
			legacyCode,
		},
		"load v4": {
			`{"version":"4"}`,
			"Lockfile version 4 in deno.lock requires npm package metadata which is not supported yet",
			legacyCode,
		},
		"load missing": {
			denoLockEmptyObj, denoLegacy1, legacyCode,
		},
		"load numeric 5": {
			`{"version":5}`,
			denoLegacy1,
			legacyCode,
		},
		"load null version": {
			`{"version":null}`,
			denoLegacy1,
			legacyCode,
		},
		"load v6":    {`{"version":"6"}`, "Unsupported lockfile version 6 in deno.lock", unsupCode},
		"load empty": {`{"version":""}`, "Unsupported lockfile version  in deno.lock", unsupCode},
		"load word":  {`{"version":"five"}`, "Unsupported lockfile version five in deno.lock", unsupCode},
		"load not object": {
			denoLockEmptyArr,
			denoLegacy1,
			legacyCode,
		},
		"load bad json":   {`{`, denoDeserPrefix, deserCode},
		"load empty text": {``, denoDeserPrefix, deserCode},
		"load trailing":   {`{"version":"5"} x`, denoDeserPrefix, deserCode},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, err := newDenoLock(tc.content)
			var d diag.Diagnostic
			if f != nil || !errors.As(err, &d) {
				t.Fatalf("got %v, %v; want a diagnostic", f, err)
			}
			if !strings.HasPrefix(d.Error(), tc.wantMsg) || d.Code() != tc.wantCode {
				t.Errorf("got %q (%s), want %q (%s)", d.Error(), d.Code(), tc.wantMsg, tc.wantCode)
			}
			if strings.HasPrefix(tc.wantMsg, "Error deserializing") && errors.Unwrap(err) == nil {
				t.Error("the JSON error is not the cause")
			}
		})
	}
}

func TestDenoLockBasics(t *testing.T) {
	t.Parallel()
	f, err := newDenoLock(`{"version":"5"}`)
	if err != nil {
		t.Fatal(err)
	}
	if f.Path() != denoLockPath {
		t.Errorf("path %q", f.Path())
	}
	if _, err := f.Version(); !errors.Is(err, versionedfile.ErrNoVersion) {
		t.Errorf("Version error %v, want ErrNoVersion", err)
	}
	// Without a dependency, and without specifiers, SetVersion is a no-op.
	g, err := f.SetVersion(mustVersion(t, denoLockV120), nil, versionedfile.Standard)
	if err != nil || g.Write() != nil {
		t.Errorf("no dependency: %v, %v", g.Write(), err)
	}
	g, err = f.SetVersion(mustVersion(t, denoLockV120), strp(denoFirst), versionedfile.Standard)
	if err != nil || g.Write() != nil {
		t.Errorf("no specifiers: %v, %v", g.Write(), err)
	}
	// A lockfile that the model rejects is still fine until something is written to it.
	bad, err := newDenoLock(`{"version":"5","redirects":5}`)
	if err != nil {
		t.Fatal(err)
	}
	if g, err = bad.SetVersion(
		mustVersion(t, denoLockV120),
		strp(denoFirst),
		versionedfile.Standard,
	); err != nil ||
		g.Write() != nil {
		t.Errorf("unrelated dependency on a bad model: %v, %v", g.Write(), err)
	}
}

func TestDenoLockFromJSONFailure(t *testing.T) {
	t.Parallel()
	content := `{"version":"5","specifiers":{"jsr:@scope/first@^1":"1.0.0"},` + denoRedirects + `}`
	f, err := newDenoLock(content)
	if err != nil {
		t.Fatal(err)
	}
	g, err := f.SetVersion(mustVersion(t, denoLockV120), strp(denoFirst), versionedfile.Standard)
	var d diag.Diagnostic
	if g != nil || !errors.As(err, &d) {
		t.Fatalf("got %v, %v; want a diagnostic", g, err)
	}
	want := "Could not update deno.lock: Invalid redirects section: invalid type: integer `5`, expected a map"
	if d.Error() != want || d.Code() != "remeris::deno_lock::update" || errors.Unwrap(err) == nil {
		t.Errorf("got %q (%s)", d.Error(), d.Code())
	}
}

// denoCase builds a small lockfile around a specifier key and the same text in the workspace.
func denoCase(key, resolved string, extra string) string {
	return `{"version":"5","specifiers":{"` + key + `":"` + resolved + `"}` + extra + `,"workspace":{"dependencies":["` + key + `"]}}`
}

func TestDenoLockSpecifierForms(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		key      string
		jsr      string
		wantKey  string
		wantDeps string
		version  string
	}{
		"form caret": {"jsr:@s/a@^1.0.0", denoSA1, denoJSRA1, denoJSRA2, npmV200},
		"form tilde": {"jsr:@s/a@~1.0.0", denoSA1, "jsr:@s/a@1.0", denoJSRA2, npmV200},
		"form exact": {"jsr:@s/a@1.0.0", denoSA1, "jsr:@s/a@1.0.0", denoJSRA2, npmV200},
		"form star":  {denoStarKey, denoSA1, denoStarKey, denoJSRA2, npmV200},
		"form bare":  {"jsr:@s/a", denoSA1, denoStarKey, denoJSRA2, npmV200},
		"form major": {denoJSRA1, denoSA1, denoJSRA1, denoJSRA2, npmV200},
		"form prerelease": {
			"jsr:@s/a@^1.0.0-rc.1",
			"@s/a@1.0.0-rc.1",
			"jsr:@s/a@^1.0.0-rc.1",
			"jsr:@s/a@1.0.0-rc.2",
			"1.0.0-rc.2",
		},
		"form npm caret":  {"npm:a@^1", "", "npm:a@1", "npm:a@2.0.0", npmV200},
		"form npm scoped": {"npm:@s/a@^1.2.3", "", "npm:@s/a@^1.2.3", "npm:@s/a@2.0.0", npmV200},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			extra, resolved := "", pomOne
			section := "npm"
			if tc.jsr != "" {
				extra, section = `,"jsr":{"`+tc.jsr+`":{"integrity":"sha256-x"}}`, "jsr"
				resolved = tc.jsr[strings.LastIndex(tc.jsr, "@")+1:]
			} else {
				nvName := strings.TrimPrefix(strings.SplitN(tc.key, ":", 2)[1], "")
				nvName = nvName[:strings.LastIndex(nvName, "@")]
				extra = `,"npm":{"` + nvName + `@1.0.0":{"integrity":"sha256-x"}}`
			}
			dep := strings.TrimPrefix(tc.wantDeps, "jsr:")
			dep = strings.TrimPrefix(dep, "npm:")
			dep = dep[:strings.LastIndex(dep, "@")]
			content, diff := lockWrite(t, setDeno(t, denoCase(tc.key, resolved, extra), [2]string{dep, tc.version}))
			if got := jsonAt(t, content, "specifiers", tc.wantKey); got != tc.version {
				t.Errorf("specifiers[%s] = %v, want %s\n%s", tc.wantKey, got, tc.version, content)
			}
			if got := jsonAt(t, content, denoWorkspace, denoDeps); !reflect.DeepEqual(got, strs(tc.wantDeps)) {
				t.Errorf("workspace dependencies %v, want %s", got, tc.wantDeps)
			}
			if got := jsonAt(t, content, section, dep+"@"+tc.version); got == nil {
				t.Errorf("no %s entry %s@%s:\n%s", section, dep, tc.version, content)
			}
			if diff != dep+" = "+tc.version {
				t.Errorf("diff %q", diff)
			}
		})
	}
}

func TestDenoLockTagsAndSkips(t *testing.T) {
	t.Parallel()
	// A tag is a requirement too; a specifier for another package is left alone.
	const content = `{"version":"5","specifiers":{"jsr:@s/a@latest":"1.0.0","jsr:@s/b@^1":"1.0.0"},` +
		`"workspace":{"dependencies":["jsr:@s/a@latest","jsr:@s/b@1"]}}`
	out, _ := lockWrite(t, setDeno(t, content, [2]string{denoSA, npmV300}))
	if got := jsonAt(t, out, "specifiers", "jsr:@s/a@latest"); got != npmV300 {
		t.Errorf("tag specifier = %v\n%s", got, out)
	}
	if got := jsonAt(t, out, "specifiers", "jsr:@s/b@1"); got != pomOne {
		t.Errorf("other package = %v", got)
	}
}

// denoBoth has the same name in the jsr and npm registries.
const denoBoth = `{"version":"5",
 "specifiers":{"jsr:@s/a@^1":"1.0.0","npm:@s/a@^1":"1.0.0"},
 "jsr":{"@s/a@1.0.0":{"integrity":"sha256-j"}},
 "npm":{"@s/a@1.0.0":{"integrity":"sha256-n"}},
 "workspace":{"dependencies":["jsr:@s/a@1.0.0","npm:@s/a@1.0.0"],
  "members":{"m":{"packageJson":{"dependencies":["npm:@s/a@^1"]}}}}}`

func TestDenoLockNameInBothRegistries(t *testing.T) {
	t.Parallel()
	out, diff := lockWrite(t, setDeno(t, denoBoth, [2]string{denoSA, v110}))
	for key, want := range map[string]string{denoJSRA1: v110, "npm:@s/a@1": v110} {
		if got := jsonAt(t, out, "specifiers", key); got != want {
			t.Errorf("specifiers[%s] = %v", key, got)
		}
	}
	for _, section := range []string{"jsr", "npm"} {
		if jsonAt(t, out, section, "@s/a@1.1.0") == nil || jsonAt(t, out, section, denoSA1) != nil {
			t.Errorf("%s not renamed:\n%s", section, out)
		}
	}
	if got := jsonAt(t, out, denoWorkspace, denoDeps); !reflect.DeepEqual(got, strs("jsr:@s/a@1.1.0", "npm:@s/a@1.1.0")) {
		t.Errorf("workspace dependencies %v", got)
	}
	if got := jsonAt(
		t,
		out,
		denoWorkspace,
		"members",
		"m",
		"packageJson",
		denoDeps,
	); !reflect.DeepEqual(
		got,
		strs("npm:@s/a@1.1.0"),
	) {
		t.Errorf("member packageJson %v", got)
	}
	if diff != "@s/a = 1.1.0" {
		t.Errorf("diff %q", diff)
	}
}

func TestDenoLockOnlyTheKindsSeen(t *testing.T) {
	t.Parallel()
	// Only the jsr specifier names the dependency of that kind: the npm registry keeps its key, but the workspace
	// rewrite does not look at kinds.
	content := strings.Replace(denoBoth, `"npm:@s/a@^1":"1.0.0"`, `"npm:@s/b@^1":"1.0.0"`, 1)
	content = strings.Replace(content, `"npm":{"@s/a@1.0.0"`, `"npm":{"@s/b@1.0.0"`, 1)
	content = strings.Replace(content, `"npm:@s/a@1.0.0"]`, `"npm:@s/b@1.0.0"]`, 1)
	content = strings.Replace(content, `"npm:@s/a@^1"`, `"npm:@s/b@^1"`, 1)
	out, _ := lockWrite(t, setDeno(t, content, [2]string{denoSA, v110}))
	if jsonAt(t, out, "npm", "@s/b@1.0.0") == nil || jsonAt(t, out, "jsr", "@s/a@1.1.0") == nil {
		t.Errorf("unexpected registries:\n%s", out)
	}
	if got := jsonAt(t, out, denoWorkspace, denoDeps); !reflect.DeepEqual(got, strs("jsr:@s/a@1.1.0", "npm:@s/b@1.0.0")) {
		t.Errorf("workspace dependencies %v", got)
	}
}

func TestDenoLockRegistryKeys(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		old     string
		resolve string
		version string
		wantKey string
		stays   []string
	}{
		"registry prerelease": {"@s/a@1.0.0-rc.1", denoRC1, "1.0.0-rc.2", "@s/a@1.0.0-rc.2", nil},
		"registry build":      {"@s/a@1.0.0+b1", "1.0.0+b1", v110, "@s/a@1.1.0", nil},
		"registry other":      {denoSA1, pomOne, v110, "@s/a@1.1.0", []string{"@s/a@0.9.0"}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			jsr := `"` + tc.old + `":{"integrity":"sha256-a"}`
			for _, s := range tc.stays {
				jsr += `,"` + s + `":{"integrity":"sha256-b"}`
			}
			content := `{"version":"5","specifiers":{"jsr:@s/a@^1":"` + tc.resolve + `"},"jsr":{` + jsr + `}}`
			out, _ := lockWrite(t, setDeno(t, content, [2]string{denoSA, tc.version}))
			if jsonAt(t, out, "jsr", tc.wantKey) == nil {
				t.Errorf("no %s:\n%s", tc.wantKey, out)
			}
			for _, s := range tc.stays {
				if jsonAt(t, out, "jsr", s) == nil {
					t.Errorf("%s was renamed:\n%s", s, out)
				}
			}
		})
	}
}

func TestDenoLockLinksAndDependents(t *testing.T) {
	t.Parallel()
	// A link is rewritten only when its own key matches; a jsr package that depends on the package follows the
	// renamed specifier; an npm package that depends on it by name follows the renamed registry key.
	const content = `{"version":"5",
 "specifiers":{"jsr:@s/a@^1":"1.0.0","jsr:@s/b@^0.1.0":"0.1.0","npm:left-pad@^1":"1.3.0","npm:app@^1":"1.0.0"},
 "jsr":{"@s/a@1.0.0":{"integrity":"sha256-a"},
  "@s/b@0.1.0":{"integrity":"sha256-b","dependencies":["jsr:@s/a@^1"]}},
 "npm":{"left-pad@1.3.0":{"integrity":"sha256-l"},
  "app@1.0.0":{"integrity":"sha256-p","dependencies":["left-pad"]}},
 "workspace":{"links":{
  "jsr:@s/a@1.0.0":{"dependencies":["jsr:@s/b@^0.1.0"]},
  "jsr:@s/b@0.1.0":{"dependencies":["jsr:@s/a@1.0.0"]}}}}`
	out, _ := lockWrite(t, setDeno(t, content, [2]string{denoSA, v110}, [2]string{denoLeftPad, denoV140}))
	if got := jsonAt(
		t,
		out,
		denoWorkspace,
		"links",
		"jsr:@s/a@1.1.0",
		denoDeps,
	); !reflect.DeepEqual(
		got,
		strs("jsr:@s/b@0.1"),
	) {
		t.Errorf("renamed link %v\n%s", got, out)
	}
	// The link that did not match keeps its (stale) dependency: only the links with a matching key are rewritten.
	// This is also why "@^0.1.0" survives in Knope's workspaces fixture.
	if got := jsonAt(
		t,
		out,
		denoWorkspace,
		"links",
		"jsr:@s/b@0.1.0",
		denoDeps,
	); !reflect.DeepEqual(
		got,
		strs("jsr:@s/a@1.0.0"),
	) {
		t.Errorf("untouched link %v\n%s", got, out)
	}
	if got := jsonAt(t, out, "jsr", "@s/b@0.1.0", denoDeps); !reflect.DeepEqual(got, strs("jsr:@s/a")) {
		t.Errorf("jsr dependent %v", got)
	}
	if got := jsonAt(t, out, "npm", "app@1.0.0", denoDeps); !reflect.DeepEqual(got, strs(denoLeftPad)) {
		t.Errorf("npm dependent %v\n%s", got, out)
	}
}

func TestDenoLockGoldens(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		name  string
		steps [][2]string
		diff  string
	}{
		"golden workspaces": {
			"workspaces", [][2]string{{"@scope/first-package", v110}, {"@scope/second-package", denoLockV011}},
			"@scope/first-package = 1.1.0, @scope/second-package = 0.1.1",
		},
		"golden file priority": {"file_priority", [][2]string{{"@scope/test-package", goV101}}, "@scope/test-package = 1.0.1"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := readDenoGolden(t, tc.name+".in.lock")
			want := readDenoGolden(t, tc.name+".out.lock")
			content, diff := lockWrite(t, setDeno(t, in, tc.steps...))
			if content != want {
				t.Errorf("output differs from Knope's:\n--- got\n%s\n--- want\n%s", content, want)
			}
			if diff != tc.diff {
				t.Errorf("diff %q, want %q", diff, tc.diff)
			}
			// A canonical lockfile that is set to the versions it already has comes out byte for byte the same.
			again, _ := lockWrite(t, setDeno(t, want, tc.steps...))
			if again != want {
				t.Errorf("not a fixed point:\n%s", again)
			}
		})
	}
}

func readDenoGolden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "deno_lock", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestDenoLockIdempotent(t *testing.T) {
	t.Parallel()
	once := setDeno(t, denoSample, [2]string{denoFirst, denoLockV120}, [2]string{denoLeftPad, denoV140})
	twice := setDeno(t, denoSample, [2]string{denoFirst, denoLockV120}, [2]string{denoLeftPad, denoV140},
		[2]string{denoFirst, denoLockV120}, [2]string{denoLeftPad, denoV140})
	a, da := lockWrite(t, once)
	b, db := lockWrite(t, twice)
	if a != b {
		t.Errorf("applying twice differs:\n%s\n---\n%s", a, b)
	}
	if da != "@scope/first = 1.2.0, left-pad = 1.4.0" || db != da+", "+da {
		t.Errorf("diffs %q, %q", da, db)
	}
	// The files are immutable: the original keeps no edit.
	base := setDeno(t, denoSample)
	if _, err := base.SetVersion(
		mustVersion(t, denoLockV120),
		strp(denoFirst),
		versionedfile.Standard,
	); err != nil ||
		base.Write() != nil {
		t.Errorf("the receiver changed: %v, %v", base.Write(), err)
	}
}

func TestDenoLockMatchesTheModel(t *testing.T) {
	t.Parallel()
	// The text written is the canonical print of the model built from it: reading it back and printing it again
	// changes nothing.
	out, _ := lockWrite(t, setDeno(t, denoSample, [2]string{denoFirst, denoLockV120}))
	c, err := denolock.Parse([]byte(out))
	if err != nil || c.Print() != out {
		t.Errorf("not canonical (%v):\n%s", err, out)
	}
	if !strings.HasSuffix(out, "}\n") || strings.Contains(out, "\r") {
		t.Errorf("want LF text with a final newline:\n%s", out)
	}
}

func FuzzDenoLock(f *testing.F) {
	for _, name := range []string{"workspaces", "file_priority"} {
		for _, kind := range []string{".in.lock", ".out.lock"} {
			if data, err := os.ReadFile(filepath.Join("testdata", "deno_lock", name+kind)); err == nil {
				f.Add(data, "@scope/first-package")
				f.Add(data, "@scope/test-package")
			}
		}
	}
	f.Add([]byte(denoSample), denoFirst)
	f.Add([]byte(denoSample), denoLeftPad)
	f.Add([]byte(denoBoth), denoSA)
	f.Fuzz(func(t *testing.T, content []byte, dep string) {
		file, err := newDenoLock(string(content))
		if err != nil {
			return
		}
		file, err = file.SetVersion(mustVersion(t, "1.2.3"), &dep, versionedfile.Standard)
		if err != nil || file.Write() == nil {
			return
		}
		out, _ := lockWrite(t, file)
		model, err := denolock.Parse([]byte(out))
		if err != nil {
			t.Fatalf("the output does not parse: %v\n%s", err, out)
		}
		if model.Print() != out {
			t.Fatalf("the output is not a fixed point:\n%s\n---\n%s", out, model.Print())
		}
	})
}
