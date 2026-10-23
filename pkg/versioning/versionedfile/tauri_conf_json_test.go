package versionedfile_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	tauriPath = "src-tauri/tauri.conf.json"
	tauriCode = "remeris::tauri_conf_json::deserialize"
	tauriHelp = "remeris expects the tauri.conf.json file to be an object with a top level `version` property"
)

const tauriTester = `{
        "productName": "tester",
        "version": "0.1.0-rc.0"
        }`

const tauriOrdered = `{
        "productName": "tester",
        "version": "0.1.0-rc.0",
        "identifier": "com.knope.tester"
        }`

func newTauri(path, content string) (versionedfile.File, error) {
	return versionedfile.New(versionedfile.Config{Path: path, Format: versionedfile.TauriConf}, content, nil)
}

func mustTauri(t *testing.T, content string) versionedfile.File {
	t.Helper()
	f, err := newTauri(tauriPath, content)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestTauriConfFormatFromPath(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"tauri.conf.json", "tauri.macos.conf.json", "tauri.windows.conf.json", "tauri." + "linux.conf.json"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			format, ok := versionedfile.FormatFromPath("a/b/" + name)
			if !ok || format != versionedfile.TauriConf {
				t.Fatalf("got %v, %v", format, ok)
			}
			f, err := newTauri("a/b/"+name, tauriTester)
			if err != nil {
				t.Fatal(err)
			}
			edited, err := f.SetVersion(mustVersion(t, npmV123rc4), nil, versionedfile.Standard)
			if err != nil {
				t.Fatal(err)
			}
			want := action.WriteToFile(
				"a/b/"+name,
				"{\n  \"productName\": \"tester\",\n  \"version\": \"1.2.3-rc.4\"\n}",
				npmV123rc4,
			)
			if got := edited.Write(); len(got) != 1 || !got[0].Equal(want) {
				t.Errorf("got %#v", got)
			}
		})
	}
	for _, name := range []string{"tauri.conf.json5", "Tauri.toml", "tauri.android.conf.json"} {
		if _, ok := versionedfile.FormatFromPath(name); ok {
			t.Errorf("%s is not supported", name)
		}
	}
}

func TestTauriConfVersion(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		content string
		want    string
	}{
		"tauri plain":          {tauriTester, npmRC0},
		"extra members":        {`{"a": [1, {"b": null}], "version": "3.2.1", "c": true}`, "3.2.1"},
		"tauri duplicate wins": {jsonDuplicateVersion, npmV200},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := mustTauri(t, tc.content)
			got, err := f.Version()
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != tc.want || f.Path() != tauriPath {
				t.Errorf("got %s at %s, want %s", got, f.Path(), tc.want)
			}
		})
	}
}

func TestTauriConfInvalid(t *testing.T) {
	t.Parallel()
	prefix := "Error deserializing " + tauriPath + ": "
	tests := map[string]string{
		"missing version":          `{"productName": "x"}`,
		"tauri non-string version": jsonNumberVersion,
		"null version":             jsonNullVersion,
		"not a semantic version":   jsonWordVersion,
		"tauri v2 package.json":    `{"version": "../package.json"}`,
		caseArray:                  `["2.0.0"]`,
		"empty input":              ``,
		"tauri syntax":             jsonTrailingComma,
		"tauri trailing text":      jsonTrailingText,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, err := newTauri(tauriPath, content)
			if f != nil || err == nil {
				t.Fatalf("got %v, %v", f, err)
			}
			if !strings.HasPrefix(err.Error(), prefix) {
				t.Errorf("message %q lacks prefix %q", err.Error(), prefix)
			}
			var d diag.Diagnostic
			if !errors.As(err, &d) {
				t.Fatalf("%T is not a diagnostic", err)
			}
			if d.Code() != tauriCode || d.Help() != tauriHelp {
				t.Errorf("code %q, help %q", d.Code(), d.Help())
			}
		})
	}
}

func TestTauriConfSetVersion(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		content string
		steps   []cargoStep
		want    string // "" means no write
		diff    string
	}{
		"tauri set version": {
			content: tauriTester,
			steps:   []cargoStep{{npmV123rc4, nil}},
			want:    "{\n  \"productName\": \"tester\",\n  \"version\": \"1.2.3-rc.4\"\n}",
			diff:    npmV123rc4,
		},
		"retain property order": {
			content: tauriOrdered,
			steps:   []cargoStep{{npmV123rc4, nil}},
			want:    "{\n  \"productName\": \"tester\",\n  \"version\": \"1.2.3-rc.4\",\n  \"identifier\": \"com.knope.tester\"\n}",
			diff:    npmV123rc4,
		},
		"tauri same version still writes": {
			content: jsonCompactOne,
			steps:   []cargoStep{{pyOne, nil}},
			want:    "{\n  \"version\": \"" + pyOne + "\"\n}",
			diff:    pyOne,
		},
		"tauri trailing newline is dropped": {
			content: jsonOneNewline,
			steps:   []cargoStep{{v110, nil}},
			want:    prettyV110,
			diff:    v110,
		},
		"a later call replaces the diff": {
			content: jsonCompactOne,
			steps:   []cargoStep{{v110, nil}, {npmV150, nil}},
			want:    "{\n  \"version\": \"1.5.0\"\n}",
			diff:    npmV150,
		},
		"a dependency is ignored": {
			content: jsonCompactOne,
			steps:   []cargoStep{{v110, strp("a")}},
			want:    prettyV110,
			diff:    v110,
		},
		"duplicate keys keep the last value": {
			content: `{"version":"1.0.0","a":1,"version":"1.0.1","a":2}`,
			steps:   []cargoStep{{v110, nil}},
			want:    "{\n  \"version\": \"1.1.0\",\n  \"a\": 2\n}",
			diff:    v110,
		},
		"nested versions are untouched": {
			content: `{"version":"1.0.0","bundle":{"version":"1.0.0"}}`,
			steps:   []cargoStep{{v110, nil}},
			want:    "{\n  \"version\": \"1.1.0\",\n  \"bundle\": {\n    \"version\": \"1.0.0\"\n  }\n}",
			diff:    v110,
		},
		"tauri numbers and unicode survive": {
			content: `{"version":"1.0.0","n":[1,1.0,1e3,-0],"s":"héllo 世 </>\u0001","csp":null}`,
			steps:   []cargoStep{{goV101, nil}},
			want: "{\n  \"version\": \"1.0.1\",\n  \"n\": [\n    1,\n    1.0,\n    1000.0,\n    -0.0\n  ],\n" +
				"  \"s\": \"héllo 世 </>\\u0001\",\n  \"csp\": null\n}",
			diff: goV101,
		},
		"no edit": {content: tauriTester},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := applySteps(t, mustTauri(t, tc.content), tc.steps).Write()
			if tc.want == "" {
				if got != nil {
					t.Fatalf("expected no write, got %v", got)
				}
				return
			}
			want := action.WriteToFile(tauriPath, tc.want, tc.diff)
			if len(got) != 1 || !got[0].Equal(want) {
				t.Errorf("got %#v\nwant %#v", got, want)
			}
		})
	}
}

func TestTauriConfImmutable(t *testing.T) {
	t.Parallel()
	orig := mustTauri(t, tauriTester)
	edited, err := orig.SetVersion(mustVersion(t, npmV200), nil, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	if orig.Write() != nil {
		t.Error("the original gained a write")
	}
	if v, _ := edited.Version(); v.String() != npmRC0 {
		t.Errorf("Version() is %s: Knope keeps the version read from the file", v)
	}
	next, err := edited.SetVersion(mustVersion(t, npmV300), nil, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	if got := edited.Write(); len(got) != 1 || got[0].Diff != npmV200 {
		t.Errorf("edited changed after a later SetVersion: %#v", got)
	}
	if got := next.Write(); len(got) != 1 || got[0].Diff != npmV300 {
		t.Errorf("next: %#v", got)
	}
}

func FuzzTauriConfSetVersion(f *testing.F) {
	for _, seed := range []string{
		tauriTester, tauriOrdered, jsonCompactOne, `{"version":"1.0.0","version":"2.0.0"}`, `{}`, `[]`, "\xef\xbb\xbf{}",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, content string) {
		file, err := newTauri(tauriPath, content)
		if err != nil {
			return
		}
		edited, err := file.SetVersion(mustVersion(t, v110), nil, versionedfile.Standard)
		if err != nil {
			t.Fatalf("SetVersion failed on a parsed file: %v", err)
		}
		written := edited.Write()
		if len(written) != 1 {
			t.Fatalf("got %d writes", len(written))
		}
		again, err := newTauri(tauriPath, written[0].Content)
		if err != nil {
			t.Fatalf("output does not parse: %v\n%s", err, written[0].Content)
		}
		if v, err := again.Version(); err != nil || v.String() != v110 {
			t.Fatalf("output has version %v, %v", v, err)
		}
	})
}
