package versionedfile_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/action"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	npmPath        = "pkg/package.json"
	npmHelp        = "remeris expects the package.json file to be an object with a top level `version` property"
	npmCode        = "remeris::package_json::deserialize"
	npmDep         = "dependency-name"
	npmDevDep      = "@dev/dependency-name"
	npmScoped      = "@scope/dependency"
	npmV123rc4     = "1.2.3-rc.4"
	npmV210        = "2.1.0"
	npmV200        = "2.0.0"
	npmRC0         = "0.1.0-rc.0"
	npmDiffV       = "version = 1.2.3-rc.4"
	npmWsCaret     = "workspace:^1.0.0"
	npmWsCaret2    = "workspace:^2.0.0"
	npmOneDep      = "{\n  \"version\": \"1.0.0\",\n  \"dependencies\": {\n    \"dependency-name\": \"%s\"\n  }\n}"
	npmPlaceholder = "%s"
	npmDiffV110    = "version = 1.1.0"
	npmV150        = "1.5.0"
	npmV300        = "3.0.0"
)

func npmWithDep(value string) string { return strings.Replace(npmOneDep, npmPlaceholder, value, 1) }

func newNpm(content string) (versionedfile.File, error) {
	return versionedfile.New(versionedfile.Config{Path: npmPath, Format: versionedfile.PackageJson}, content, nil)
}

func mustNpm(t *testing.T, content string) versionedfile.File {
	t.Helper()
	f, err := newNpm(content)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

const npmTester = "{\n        \"name\": \"tester\",\n        \"version\": \"0.1.0-rc.0\"\n        }"

const npmBoth = `{
            "name": "tester",
            "version": "1.0.0",
            "dependencies": {
                "dependency-name": "2.0.0",
                "some-other-dependency": "0.1.0"
            },
            "devDependencies": {
                "@another/dev-dependency": "0.2.0",
                "@dev/dependency-name": "3.0.0"
            }
        }`

func TestPackageJSONVersion(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		content string
		want    string
	}{
		"plain":            {npmTester, npmRC0},
		"extra members":    {`{"a": [1, {"b": null}], "version": "3.2.1", "c": true}`, "3.2.1"},
		"duplicate wins":   {`{"version": "1.0.0", "version": "2.0.0"}`, npmV200},
		"trailing newline": {"{\"version\": \"0.0.1\"}\n", "0.0.1"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := mustNpm(t, tc.content).Version()
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
			if p := mustNpm(t, tc.content).Path(); p != npmPath {
				t.Errorf("Path() = %q", p)
			}
		})
	}
}

func TestPackageJSONInvalid(t *testing.T) {
	t.Parallel()
	prefix := "Error deserializing " + npmPath + ": "
	tests := map[string]string{
		"missing version":        `{"name": "x"}`,
		"non-string version":     `{"version": 1}`,
		"null version":           `{"version": null}`,
		"not a semantic version": `{"version": "one"}`,
		"array":                  `["1.0.0"]`,
		"string":                 `"1.0.0"`,
		"empty input":            ``,
		"syntax":                 `{"version": "1.0.0",}`,
		"trailing text":          `{"version": "1.0.0"} x`,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, err := newNpm(content)
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
			if d.Code() != npmCode || d.Help() != npmHelp {
				t.Errorf("code %q, help %q", d.Code(), d.Help())
			}
		})
	}
}

func TestPackageJSONSetVersion(t *testing.T) {
	t.Parallel()
	dep := strp(npmDep)
	tests := map[string]struct {
		content string
		steps   []cargoStep
		want    string // "" means no write
		diff    string
	}{
		"set version": {
			content: npmTester,
			steps:   []cargoStep{{npmV123rc4, nil}},
			want:    "{\n  \"name\": \"tester\",\n  \"version\": \"1.2.3-rc.4\"\n}",
			diff:    npmDiffV,
		},
		"retain property order": {
			content: "{\n        \"name\": \"tester\",\n        \"version\": \"0.1.0-rc.0\",\n        \"dependencies\": {}\n        }",
			steps:   []cargoStep{{npmV123rc4, nil}},
			want:    "{\n  \"name\": \"tester\",\n  \"version\": \"1.2.3-rc.4\",\n  \"dependencies\": {}\n}",
			diff:    npmDiffV,
		},
		"same version still writes": {
			content: `{"version":"1.0.0"}`,
			steps:   []cargoStep{{pyOne, nil}},
			want:    "{\n  \"version\": \"1.0.0\"\n}",
			diff:    "version = 1.0.0",
		},
		"trailing newline is dropped": {
			content: "{\"version\": \"1.0.0\"}\n",
			steps:   []cargoStep{{v110, nil}},
			want:    "{\n  \"version\": \"1.1.0\"\n}",
			diff:    npmDiffV110,
		},
		"dependency in dependencies": {
			content: npmBoth,
			steps:   []cargoStep{{npmV210, dep}},
			want: "{\n  \"name\": \"tester\",\n  \"version\": \"1.0.0\",\n  \"dependencies\": {\n" +
				"    \"dependency-name\": \"2.1.0\",\n    \"some-other-dependency\": \"0.1.0\"\n  },\n" +
				"  \"devDependencies\": {\n    \"@another/dev-dependency\": \"0.2.0\",\n    \"@dev/dependency-name\": \"3.0.0\"\n  }\n}",
			diff: "dependency-name = 2.1.0",
		},
		"dependency in devDependencies": {
			content: npmBoth,
			steps:   []cargoStep{{"3.1.0", strp(npmDevDep)}},
			want: "{\n  \"name\": \"tester\",\n  \"version\": \"1.0.0\",\n  \"dependencies\": {\n" +
				"    \"dependency-name\": \"2.0.0\",\n    \"some-other-dependency\": \"0.1.0\"\n  },\n" +
				"  \"devDependencies\": {\n    \"@another/dev-dependency\": \"0.2.0\",\n    \"@dev/dependency-name\": \"3.1.0\"\n  }\n}",
			diff: "@dev/dependency-name = 3.1.0",
		},
		"dependency in both sections": {
			content: `{"version":"1.0.0","dependencies":{"a":"1.0.0"},"devDependencies":{"a":"workspace:~1.0.0"}}`,
			steps:   []cargoStep{{npmV150, strp("a")}},
			want: "{\n  \"version\": \"1.0.0\",\n  \"dependencies\": {\n    \"a\": \"1.5.0\"\n  },\n" +
				"  \"devDependencies\": {\n    \"a\": \"workspace:~1.5.0\"\n  }\n}",
			diff: "a = workspace:~1.5.0", // the last section's value, like Knope
		},
		"dependency absent": {
			content: npmBoth,
			steps:   []cargoStep{{npmV210, strp("nope")}},
		},
		"dependency without sections": {
			content: npmTester,
			steps:   []cargoStep{{npmV210, dep}},
		},
		"dependency sections are not objects": {
			content: `{"version":"1.0.0","dependencies":["dependency-name"],"devDependencies":"x"}`,
			steps:   []cargoStep{{npmV210, dep}},
		},
		"dependency value is not a string": {
			content: `{"version":"1.0.0","dependencies":{"dependency-name":{"version":"1.0.0"}}}`,
			steps:   []cargoStep{{npmV210, dep}},
		},
		"dependency already at the version": {
			content: npmWithDep(npmV210),
			steps:   []cargoStep{{npmV210, dep}},
		},
		"workspace caret": {
			content: npmWithDep("workspace:^1.2.3"),
			steps:   []cargoStep{{npmV200, dep}},
			want:    npmWithDep("workspace:^2.0.0"),
			diff:    "dependency-name = workspace:^2.0.0",
		},
		"workspace tilde": {
			content: npmWithDep("workspace:~1.0.0"),
			steps:   []cargoStep{{npmV200, dep}},
			want:    npmWithDep("workspace:~2.0.0"),
			diff:    "dependency-name = workspace:~2.0.0",
		},
		"workspace exact": {
			content: npmWithDep("workspace:1.0.0"),
			steps:   []cargoStep{{npmV200, dep}},
			want:    npmWithDep("workspace:2.0.0"),
			diff:    "dependency-name = workspace:2.0.0",
		},
		"workspace comparator": {
			content: npmWithDep("workspace:>=1.0.0"),
			steps:   []cargoStep{{npmV200, dep}},
			want:    npmWithDep("workspace:>=2.0.0"),
			diff:    "dependency-name = workspace:>=2.0.0",
		},
		"workspace star": {
			content: npmWithDep("workspace:*"),
			steps:   []cargoStep{{npmV200, dep}},
		},
		"workspace bare caret": {
			content: npmWithDep("workspace:^"),
			steps:   []cargoStep{{npmV200, dep}},
		},
		"workspace empty": {
			content: npmWithDep("workspace:"),
			steps:   []cargoStep{{npmV200, dep}},
		},
		"workspace already current": {
			content: npmWithDep(npmWsCaret2),
			steps:   []cargoStep{{npmV200, dep}},
		},
		"range becomes the version": {
			content: npmWithDep("^1.0.0"),
			steps:   []cargoStep{{npmV200, dep}},
			want:    npmWithDep(npmV200),
			diff:    "dependency-name = 2.0.0",
		},
		"star becomes the version": {
			content: npmWithDep("*"),
			steps:   []cargoStep{{npmV200, dep}},
			want:    npmWithDep(npmV200),
			diff:    "dependency-name = 2.0.0",
		},
		"workspace dependency": {
			content: `{"version":"1.0.0","dependencies":{"@scope/dependency":"workspace:^1.0.0"}}`,
			steps:   []cargoStep{{npmV200, strp(npmScoped)}},
			want:    "{\n  \"version\": \"1.0.0\",\n  \"dependencies\": {\n    \"@scope/dependency\": \"workspace:^2.0.0\"\n  }\n}",
			diff:    "@scope/dependency = workspace:^2.0.0",
		},
		"version then dependency": {
			content: npmWithDep(npmWsCaret),
			steps:   []cargoStep{{npmV150, nil}, {npmV150, dep}},
			want:    "{\n  \"version\": \"1.5.0\",\n  \"dependencies\": {\n    \"dependency-name\": \"workspace:^1.5.0\"\n  }\n}",
			diff:    "version = 1.5.0, dependency-name = workspace:^1.5.0",
		},
		"two dependencies": {
			content: `{"version":"1.0.0","dependencies":{"a":"1.0.0","b":"1.0.0"}}`,
			steps:   []cargoStep{{v110, strp("a")}, {"1.2.0", strp("b")}},
			want:    "{\n  \"version\": \"1.0.0\",\n  \"dependencies\": {\n    \"a\": \"1.1.0\",\n    \"b\": \"1.2.0\"\n  }\n}",
			diff:    "a = 1.1.0, b = 1.2.0",
		},
		"unchanged dependency leaves the diff of an earlier edit": {
			content: npmWithDep(npmV200),
			steps:   []cargoStep{{v110, nil}, {npmV200, dep}},
			want:    strings.Replace(npmWithDep(npmV200), `"1.0.0",`, `"1.1.0",`, 1),
			diff:    npmDiffV110,
		},
		"numbers and unicode survive": {
			content: `{"version":"1.0.0","n":[1,1.0,1e3,-0,18446744073709551615,1.5e-7],"s":"héllo 世 </>\u0001"}`,
			steps:   []cargoStep{{"1.0.1", nil}},
			want: "{\n  \"version\": \"1.0.1\",\n  \"n\": [\n    1,\n    1.0,\n    1000.0,\n    -0.0,\n    18446744073709551615,\n    1.5e-7\n  ],\n" +
				"  \"s\": \"héllo 世 </>\\u0001\"\n}",
			diff: "version = 1.0.1",
		},
		"version stays in place": {
			content: `{"a":1,"version":"1.0.0","b":2}`,
			steps:   []cargoStep{{"1.0.1", nil}},
			want:    "{\n  \"a\": 1,\n  \"version\": \"1.0.1\",\n  \"b\": 2\n}",
			diff:    "version = 1.0.1",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := applySteps(t, mustNpm(t, tc.content), tc.steps)
			got := f.Write()
			if tc.want == "" {
				if got != nil {
					t.Fatalf("expected no write, got %v", got)
				}
				return
			}
			want := []action.Action{action.WriteToFile(npmPath, tc.want, tc.diff)}
			if len(got) != 1 || !got[0].Equal(want[0]) {
				t.Errorf("got %#v\nwant %#v", got, want)
			}
		})
	}
}

func TestPackageJSONImmutable(t *testing.T) {
	t.Parallel()
	orig := mustNpm(t, npmWithDep(pyOne))
	edited, err := orig.SetVersion(mustVersion(t, npmV200), strp(npmDep), versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	if orig.Write() != nil {
		t.Error("the original gained a write")
	}
	next, err := edited.SetVersion(mustVersion(t, npmV300), nil, versionedfile.Standard)
	if err != nil {
		t.Fatal(err)
	}
	got := edited.Write()
	if len(got) != 1 || !strings.HasSuffix(got[0].Diff, "dependency-name = 2.0.0") ||
		strings.Contains(got[0].Diff, "version =") {
		t.Errorf("edited changed after a later SetVersion: %#v", got)
	}
	if v, _ := orig.Version(); v.String() != pyOne {
		t.Errorf("original version is %s", v)
	}
	if v, _ := next.Version(); v.String() != npmV300 {
		t.Errorf("next version is %s", v)
	}
	if v, _ := edited.Version(); v.String() != pyOne {
		t.Errorf("a dependency edit changed the version to %s", v)
	}
}

func TestPackageJSONFixtures(t *testing.T) {
	t.Parallel()
	// Fixtures of Knope's prepare_release/npm: dependencies, package and workspaces/first.
	const dependencies = "{\n  \"name\": \"test-package\",\n  \"version\": \"%s\",\n  \"dependencies\": {\n" +
		"    \"dependency-name\": \"%s\"\n  },\n  \"devDependencies\": {\n    \"dev-dependency-name\": \"%s\"\n  }\n}"
	const first = "{\n  \"name\": \"firstPackage\",\n  \"version\": \"%s\",\n  \"dependencies\": {\n" +
		"    \"secondPackage\": \"%s\",\n    \"thirdPackage\": \"%s\"\n  }\n}"
	tests := map[string]struct {
		in, out string
		steps   []cargoStep
		diff    string
	}{
		"dependencies fixture": {
			in:    fmt.Sprintf(dependencies, pyOne, pyOne, pyOne),
			out:   fmt.Sprintf(dependencies, pyOne, v110, v110),
			steps: []cargoStep{{v110, strp(npmDep)}, {v110, strp("dev-dependency-name")}},
			diff:  "dependency-name = 1.1.0, dev-dependency-name = 1.1.0",
		},
		"package": {
			in:    fmt.Sprintf(dependencies, pyOne, pyOne, pyOne),
			out:   fmt.Sprintf(dependencies, v110, pyOne, pyOne),
			steps: []cargoStep{{v110, nil}},
			diff:  npmDiffV110,
		},
		"workspaces first": {
			in:    fmt.Sprintf(first, "0.1.0", "2.0.0", npmV300),
			out:   fmt.Sprintf(first, "0.2.0", "2.1.0", "3.0.1"),
			steps: []cargoStep{{"0.2.0", nil}, {"2.1.0", strp("secondPackage")}, {"3.0.1", strp("thirdPackage")}},
			diff:  "version = 0.2.0, secondPackage = 2.1.0, thirdPackage = 3.0.1",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := applySteps(t, mustNpm(t, tc.in), tc.steps).Write()
			if len(got) != 1 || !got[0].Equal(action.WriteToFile(npmPath, tc.out, tc.diff)) {
				t.Errorf("got %#v", got)
			}
		})
	}
}
