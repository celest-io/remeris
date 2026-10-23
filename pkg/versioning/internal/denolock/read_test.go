package denolock

import (
	"errors"
	"strconv"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/internal/denosemver"
)

// TestFromJSONErrors checks every error that from_json can return, with the text of the Rust error (deno_lockfile
// 0.49.0 error.rs, and serde / serde_json for the shape errors of a section).
func TestFromJSONErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		in   string
		kind ErrorKind
		want string
	}{
		"specifiers not a map": {
			`{"specifiers": 5}`, FailedDeserializing,
			"Invalid specifiers section: invalid type: integer `5`, expected a map",
		},
		"specifiers null": {
			`{"specifiers": null}`, FailedDeserializing,
			"Invalid specifiers section: invalid type: null, expected a map",
		},
		"specifier value not a string": {
			`{"specifiers": {"jsr:a@1": 1}}`, FailedDeserializing,
			"Invalid specifiers section: invalid type: integer `1`, expected a string",
		},
		"specifier key with another scheme": {
			`{"specifiers": {"x:foo": "1"}}`, InvalidPackageSpecifier,
			"Unexpected JSR dependency scheme 'x'. Expected 'npm:' or 'jsr:'",
		},
		"specifier key without a scheme": {
			`{"specifiers": {"a": "1"}}`, InvalidPackageSpecifier,
			"Unexpected JSR dependency scheme 'a'. Expected 'npm:' or 'jsr:'",
		},
		"specifier key with a scope and no name": {
			`{"specifiers": {"jsr:@a": "1"}}`, InvalidPackageSpecifier,
			"Invalid package requirement '@a'",
		},
		"specifier key with a bad requirement": {
			`{"specifiers": {"jsr:a@^^": "1"}}`, InvalidPackageSpecifier,
			"Invalid package requirement 'a@^^'",
		},
		"npm entry not a struct": {
			`{"npm": {"a@1.0.0": 1}}`, FailedDeserializing,
			"Invalid npm section: invalid type: integer `1`, expected struct RawNpmPackageInfo",
		},
		"npm not a map": {
			`{"npm": []}`, FailedDeserializing,
			"Invalid npm section: invalid type: sequence, expected a map",
		},
		"npm os not a sequence": {
			`{"npm": {"a@1.0.0": {"os": "linux"}}}`, FailedDeserializing,
			`Invalid npm section: invalid type: string "linux", expected a sequence`,
		},
		"npm os element not a string": {
			`{"npm": {"a@1.0.0": {"os": [true]}}}`, FailedDeserializing,
			"Invalid npm section: invalid type: boolean `true`, expected a string",
		},
		"npm deprecated not a boolean": {
			`{"npm": {"a@1.0.0": {"deprecated": "yes"}}}`, FailedDeserializing,
			`Invalid npm section: invalid type: string "yes", expected a boolean`,
		},
		"npm integrity not a string": {
			`{"npm": {"a@1.0.0": {"integrity": 5}}}`, FailedDeserializing,
			"Invalid npm section: invalid type: integer `5`, expected a string",
		},
		"npm dependencies null": {
			`{"npm": {"a@1.0.0": {"dependencies": null}}}`, FailedDeserializing,
			"Invalid npm section: invalid type: null, expected a sequence",
		},
		"npm id without a version": {
			`{"npm": {"nover": {}}}`, InvalidNpmPackageID,
			"Invalid npm package 'nover'",
		},
		"npm id that is empty": {
			`{"npm": {"": {}}}`, InvalidNpmPackageID,
			"Invalid npm package ''",
		},
		"npm id with an at sign only first": {
			`{"npm": {"@scope/name": {}}}`, InvalidNpmPackageID,
			"Invalid npm package '@scope/name'",
		},
		"npm dependency that is not in the section": {
			`{"npm": {"a@1.0.0": {"dependencies": ["zzz"]}}}`, MissingPackage,
			msgMissingZzz,
		},
		"npm optional dependency that is not in the section": {
			`{"npm": {"a@1.0.0": {"optionalDependencies": ["zzz"]}}}`, MissingPackage,
			msgMissingZzz,
		},
		"npm optional peer that is not in the section": {
			`{"npm": {"a@1.0.0": {"optionalPeers": ["zzz"]}}}`, MissingPackage,
			msgMissingZzz,
		},
		"npm alias without a version": {
			`{"npm": {"a@1.0.0": {"dependencies": ["b@npm:c"]}}}`, InvalidNpmPackageDependency,
			"Invalid npm package dependency 'b@npm:c'",
		},
		"jsr key that is not a name and version": {
			`{"jsr": {"bad": {"integrity": "x"}}}`, FailedDeserializing,
			"Invalid jsr section: Invalid package name and version 'bad'. Unexpected character.\n  bad\n  ~",
		},
		"jsr key with a bad version": {
			`{"jsr": {"a@x": {"integrity": "x"}}}`, FailedDeserializing,
			"Invalid jsr section: Invalid package name and version 'a@x'. Invalid npm version\n  x\n  ~",
		},
		"jsr entry without integrity": {
			`{"jsr": {"a@1.0.0": {}}}`, FailedDeserializing,
			"Invalid jsr section: missing field `integrity`",
		},
		"jsr entry not a struct": {
			`{"jsr": {"a@1.0.0": "x"}}`, FailedDeserializing,
			`Invalid jsr section: invalid type: string "x", expected struct RawJsrPackageInfo`,
		},
		"jsr dependency without a specifier": {
			`{"jsr": {"a@1.0.0": {"integrity": "x", "dependencies": ["jsr:b"]}}}`, InvalidJSRDependency,
			"Invalid jsr dependency 'jsr:b' for 'a@1.0.0'",
		},
		"jsr dependency with another requirement than the specifier": {
			`{"specifiers": {"jsr:b@^1": "1.0.0"}, "jsr": {"a@1.0.0": {"integrity": "x", "dependencies": ["jsr:b@^2"]}}}`,
			InvalidJSRDependency,
			"Invalid jsr dependency 'jsr:b@^2' for 'a@1.0.0'",
		},
		"redirects not a map": {
			`{"redirects": []}`, FailedDeserializing,
			"Invalid redirects section: invalid type: sequence, expected a map",
		},
		"remote value not a string": {
			`{"remote": {"https://a": true}}`, FailedDeserializing,
			"Invalid remote section: invalid type: boolean `true`, expected a string",
		},
		"a float where a string is expected": {
			`{"remote": {"https://a": 1.5}}`, FailedDeserializing,
			"Invalid remote section: invalid type: floating point `1.5`, expected a string",
		},
		"a float with no fraction": {
			`{"remote": {"https://a": 2.0}}`, FailedDeserializing,
			"Invalid remote section: invalid type: floating point `2.0`, expected a string",
		},
		"a negative integer": {
			`{"remote": {"https://a": -7}}`, FailedDeserializing,
			"Invalid remote section: invalid type: integer `-7`, expected a string",
		},
		"a map where a string is expected": {
			`{"specifiers": {"jsr:a@1": "1"}, "remote": {"https://a": {"b": 1}}}`, FailedDeserializing,
			"Invalid remote section: invalid type: map, expected a string",
		},
		"workspace not a struct": {
			`{"workspace": 5}`, FailedDeserializing,
			"Invalid workspace section: invalid type: integer `5`, expected struct WorkspaceConfigContent",
		},
		"workspace links and patches": {
			`{"workspace": {"links": {}, "patches": {}}}`, FailedDeserializing,
			"Invalid workspace section: duplicate field `links`",
		},
		"workspace members not a map": {
			`{"workspace": {"members": []}}`, FailedDeserializing,
			"Invalid workspace section: invalid type: sequence, expected a map",
		},
		"workspace member not a struct": {
			`{"workspace": {"members": {"a": []}}}`, FailedDeserializing,
			"Invalid workspace section: invalid type: sequence, expected struct WorkspaceMemberConfigContent",
		},
		"workspace dependency with another scheme": {
			`{"workspace": {"dependencies": ["foo"]}}`, FailedDeserializing,
			"Invalid workspace section: Unexpected JSR dependency scheme 'foo'. Expected 'npm:' or 'jsr:'",
		},
		"workspace dependency with a bad requirement": {
			`{"workspace": {"dependencies": ["jsr:@a/b@x y"]}}`, FailedDeserializing,
			"Invalid workspace section: Invalid package requirement '@a/b@x y'",
		},
		"workspace dependencies not a sequence": {
			`{"workspace": {"dependencies": {}}}`, FailedDeserializing,
			"Invalid workspace section: invalid type: map, expected a sequence",
		},
		"workspace dependency not a string": {
			`{"workspace": {"dependencies": [1]}}`, FailedDeserializing,
			"Invalid workspace section: invalid type: integer `1`, expected a string",
		},
		"workspace link not a struct": {
			`{"workspace": {"links": {"a": 1}}}`, FailedDeserializing,
			"Invalid workspace section: invalid type: integer `1`, expected struct LockfileLinkContent",
		},
		"workspace packageJson not a struct": {
			`{"workspace": {"packageJson": 1}}`, FailedDeserializing,
			"Invalid workspace section: invalid type: integer `1`, expected struct LockfilePackageJsonContent",
		},
		"workspace member packageJson null": {
			`{"workspace": {"members": {"a": {"packageJson": null}}}}`, FailedDeserializing,
			"Invalid workspace section: invalid type: null, expected struct LockfilePackageJsonContent",
		},
		"workspace link peerDependenciesMeta not a map": {
			`{"workspace": {"links": {"a": {"peerDependenciesMeta": []}}}}`, FailedDeserializing,
			"Invalid workspace section: invalid type: sequence, expected a map",
		},
		"sections are read in the order of the Rust code": {
			`{"workspace": 5, "jsr": 5, "npm": 5, "specifiers": 5}`, FailedDeserializing,
			"Invalid specifiers section: invalid type: integer `5`, expected a map",
		},
		"npm before jsr": {
			`{"workspace": 5, "jsr": 5, "npm": 5}`, FailedDeserializing,
			"Invalid npm section: invalid type: integer `5`, expected a map",
		},
		"jsr before redirects": {
			`{"redirects": 5, "jsr": 5}`, FailedDeserializing,
			"Invalid jsr section: invalid type: integer `5`, expected a map",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root, err := ParseJSON([]byte(tt.in))
			if err != nil {
				t.Fatal(err)
			}
			_, err = FromJSON(root)
			var de *DeserializationError
			if !errors.As(err, &de) {
				t.Fatalf("want a *DeserializationError, got %v", err)
			}
			if de.Kind != tt.kind || de.Error() != tt.want {
				t.Fatalf("got kind %d %q\nwant kind %d %q", de.Kind, de.Error(), tt.kind, tt.want)
			}
			if tt.kind == InvalidPackageSpecifier && de.Unwrap() == nil {
				t.Fatal("the denosemver error should be reachable with errors.Unwrap")
			}
		})
	}
}

// TestFromJSONAccepts lists inputs that are not errors although they look odd: from_json is lenient.
func TestFromJSONAccepts(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"an array":          emptyArray,
		"a number":          `5`,
		"null":              jsonNull,
		"a string":          `"deno"`,
		"an empty object":   emptyObject,
		"a missing version": `{"remote": {}}`,
		"another version":   `{"version": "4"}`,
		"a numeric version": `{"version": 5}`,
		"unknown sections":  `{"version": "5", "other": 1, "workspace": {"unknown": 1}}`,
		"a jsr dependency that fails the strict parse": `{"specifiers": {"jsr:a@1": "1.0.0"}, "jsr": {"a@1.0.0": {"integrity": "x", "dependencies": ["x:y", "jsr:"]}}}`,
		"an empty npm":               `{"npm": {}}`,
		"null overrides":             `{"workspace": {"packageJson": {"overrides": null}}}`,
		"an unknown key in a struct": `{"npm": {"a@1.0.0": {"future": [1, 2]}}}`,
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root, err := ParseJSON([]byte(in))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := FromJSON(root); err != nil {
				t.Fatalf("FromJSON: %v", err)
			}
		})
	}
}

func TestVersion(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		in   string
		want string
		ok   bool
	}{
		"five":              {`{"version": "5"}`, "5", true},
		"four":              {`{"version": "4"}`, "4", true},
		"missing":           {emptyObject, "", false},
		"not a string":      {`{"version": 5}`, "", false},
		"null":              {`{"version": null}`, "", false},
		"not an object":     {emptyArray, "", false},
		"a future version":  {`{"version": "2000"}`, "2000", true},
		"the last one wins": {`{"version": "4", "version": "5"}`, "5", true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root, err := ParseJSON([]byte(tt.in))
			if err != nil {
				t.Fatal(err)
			}
			got, ok := Version(root)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("Version = %q, %v; want %q, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

// TestParseJSON checks the parts of serde_json that the lockfile reader relies on.
func TestParseJSON(t *testing.T) {
	t.Parallel()
	t.Run("duplicate names keep the last value at the first position", func(t *testing.T) {
		t.Parallel()
		v, err := ParseJSON([]byte(`{"a": 1, "b": 2, "a": 3}`))
		if err != nil {
			t.Fatal(err)
		}
		if len(v.Obj) != 2 || v.Obj[0].Key != "a" || v.Obj[0].Value.Str != "3" || v.Obj[1].Key != "b" {
			t.Fatalf("got %+v", v.Obj)
		}
	})
	t.Run("a large object keeps the same rule", func(t *testing.T) {
		t.Parallel()
		src := "{"
		for i := range 40 {
			src += `"k` + strconv.Itoa(i) + `": 0,`
		}
		src += `"k0": 1, "k39": 2}`
		v, err := ParseJSON([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		if first := v.Obj[0]; first.Key != "k0" || first.Value.Str != "1" {
			t.Fatalf("first member %+v", first)
		}
		if len(v.Obj) != 40 {
			t.Fatalf("%d members, want 40", len(v.Obj))
		}
	})
	t.Run("the recursion limit", func(t *testing.T) {
		t.Parallel()
		deep := func(n int) []byte {
			b := make([]byte, 0, 2*n)
			for range n {
				b = append(b, '[')
			}
			for range n {
				b = append(b, ']')
			}
			return b
		}
		if _, err := ParseJSON(deep(127)); err != nil {
			t.Fatalf("127 levels: %v", err)
		}
		if _, err := ParseJSON(deep(128)); err == nil || err.Error() != "recursion limit exceeded" {
			t.Fatalf("128 levels: %v", err)
		}
	})
	t.Run("syntax errors", func(t *testing.T) {
		t.Parallel()
		for _, in := range []string{``, `{`, `{"a": }`, `{} {}`, `[1,]`, "\"\xff\"", `{"a": 1e999}`} {
			if _, err := ParseJSON([]byte(in)); err == nil {
				t.Errorf("%q: want an error", in)
			}
		}
	})
}

// TestSpecifierDuplicates checks HashMap::insert: two keys that are one requirement keep the first key and the last
// value, in the sorted order of the keys.
func TestSpecifierDuplicates(t *testing.T) {
	t.Parallel()
	c, err := Parse(
		[]byte(`{"specifiers": {"jsr:a@^1.0.0": "1.0.0", "jsr:a@1": "1.1.0", "jsr:a": "2.0.0", "jsr:a@*": "3.0.0"}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Specifiers) != 2 {
		t.Fatalf("got %d specifiers: %+v", len(c.Specifiers), c.Specifiers)
	}
	// "jsr:a" < "jsr:a@*" < "jsr:a@1" < "jsr:a@^1.0.0"
	first, second := c.Specifiers[0], c.Specifiers[1]
	if first.Req.Req.VersionReq.VersionText() != "*" || first.Resolved != "3.0.0" {
		t.Errorf("first: %q %q", first.Req.Req.VersionReq.VersionText(), first.Resolved)
	}
	if second.Req.Req.VersionReq.VersionText() != "1" || second.Resolved != "1.0.0" {
		t.Errorf("second: %q %q", second.Req.Req.VersionReq.VersionText(), second.Resolved)
	}
}

// TestReadResolvesDependencies checks how the dependencies of jsr and npm packages are resolved on reading.
func TestReadResolvesDependencies(t *testing.T) {
	t.Parallel()
	in := `{
  "specifiers": {"jsr:@a/one@^1": "1.0.0", "jsr:@a/two@^2": "2.0.0", "jsr:@a/two@^3": "3.0.0", "npm:three@^1": "1.0.0"},
  "jsr": {"@a/one@1.0.0": {"integrity": "x", "dependencies": ["jsr:@a/one", "jsr:@a/two@^3", "jsr:@a/two@^3.0.0", "npm:three"]}},
  "npm": {
    "alias-target@2.0.0": {},
    "three@1.0.0": {"dependencies": ["alias@npm:alias-target@2.0.0", "three", "three@1.0.0"]}
  }
}`
	c, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range c.JSR[0].Dependencies {
		got = append(got, d.String())
	}
	// the bare "jsr:@a/one" is the one specifier of that name, "jsr:@a/two@^3" and "^3.0.0" are the same
	// requirement (one entry), and the bare "npm:three" is the one npm specifier
	want := []string{"jsr:@a/one@^1", "jsr:@a/two@^3", "npm:three@^1"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	deps := c.NPM[1].Dependencies
	if len(deps) != 2 || deps[0] != (NPMDep{"alias", "alias-target@2.0.0"}) || deps[1] != (NPMDep{"three", "three@1.0.0"}) {
		t.Fatalf("npm dependencies: %+v", deps)
	}
}

// TestParsePackageNvKeys checks that the keys of jsr use the PackageNv grammar, so that an npm peer suffix is not one.
func TestParsePackageNvKeys(t *testing.T) {
	t.Parallel()
	if _, err := Parse([]byte(`{"jsr": {"@a/b@1.0.0_c@1": {"integrity": "x"}}}`)); err == nil {
		t.Fatal("a peer suffix in a jsr key should fail")
	}
	var nvErr *denosemver.PackageNvParseError
	_, err := denosemver.ParsePackageNv("a@1.0.0_c@1")
	if !errors.As(err, &nvErr) {
		t.Fatalf("denosemver: %v", err)
	}
}
