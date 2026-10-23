package denolock

import (
	"strings"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/internal/denosemver"
)

const (
	header    = "{\n  \"version\": \"5\""
	onlyFive  = header + "\n}\n"
	fiveComma = header + ",\n"
)

func parseOrFatal(t *testing.T, in string) *Content {
	t.Helper()
	c, err := Parse([]byte(in))
	if err != nil {
		t.Fatalf("Parse: %v\n%s", err, in)
	}
	return c
}

// TestPrint reads lockfiles that are not canonical and checks the exact text, then that the text is a fixed point.
func TestPrint(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		in   string
		want string
	}{
		"empty sections are left out": {
			`{"version":"5","specifiers":{},"jsr":{},"npm":{},"redirects":{},"remote":{},"workspace":{}}`,
			onlyFive,
		},
		"an empty workspace with empty parts is left out": {
			`{"workspace":{"dependencies":[],"packageJson":{"dependencies":[]},"members":{},"links":{}}}`,
			onlyFive,
		},
		"unknown top-level keys are dropped": {
			`{"zzz":1,"version":"5","extra":{"a":[1]},"remote":{"https://b":"2","https://a":"1"}}`,
			fiveComma + "  \"remote\": {\n    \"https://a\": \"1\",\n    \"https://b\": \"2\"\n  }\n}\n",
		},
		"the version is always 5":       {`{"version":"4"}`, onlyFive},
		"a missing version":             {emptyObject, onlyFive},
		"a value that is not an object": {`[1, 2]`, onlyFive},
		"keys come in the order of the printer": {
			`{"workspace":{"links":{"jsr:a@1":{}}},"remote":{"r":"1"},"redirects":{"u":"v"},` +
				`"npm":{"n@1.0.0":{}},"jsr":{"j@1.0.0":{"integrity":"i"}},"specifiers":{"jsr:j@1":"1.0.0"}}`,
			fiveComma +
				"  \"specifiers\": {\n    \"jsr:j@1\": \"1.0.0\"\n  },\n" +
				"  \"jsr\": {\n    \"j@1.0.0\": {\n      \"integrity\": \"i\"\n    }\n  },\n" +
				"  \"npm\": {\n    \"n@1.0.0\": {}\n  },\n" +
				"  \"redirects\": {\n    \"u\": \"v\"\n  },\n" +
				"  \"remote\": {\n    \"r\": \"1\"\n  },\n" +
				"  \"workspace\": {\n    \"links\": {\n      \"jsr:a@1\": {}\n    }\n  }\n}\n",
		},
		"specifiers are written in the normalised form and sorted by that text": {
			`{"specifiers":{"npm:left-pad@^1":"1.3.0","jsr:b@^1.2.3":"1.2.3","jsr:@a/z@1":"1.0.0","jsr:@a/z@*":"2.0.0"}}`,
			fiveComma + "  \"specifiers\": {\n    \"jsr:@a/z@*\": \"2.0.0\",\n    \"jsr:@a/z@1\": \"1.0.0\",\n" +
				"    \"jsr:b@^1.2.3\": \"1.2.3\",\n    \"npm:left-pad@1\": \"1.3.0\"\n  }\n}\n",
		},
		"two spellings of one requirement are one entry": {
			`{"specifiers":{"npm:x@^1.0.0":"1.0.0","npm:x@1":"1.1.0","jsr:y":"3.0.0","jsr:y@*":"2.0.0"}}`,
			fiveComma + "  \"specifiers\": {\n    \"jsr:y@*\": \"2.0.0\",\n    \"npm:x@1\": \"1.0.0\"\n  }\n}\n",
		},
		"a jsr dependency is bare when its name has one specifier": {
			`{"specifiers":{"jsr:@a/b@^1":"1.0.0","jsr:@a/c@^1":"1.0.0"},"jsr":{` +
				`"@a/b@1.0.0":{"integrity":"x","dependencies":["jsr:@a/c@^1"]},"@a/c@1.0.0":{"integrity":"y"}}}`,
			fiveComma + "  \"specifiers\": {\n    \"jsr:@a/b@1\": \"1.0.0\",\n    \"jsr:@a/c@1\": \"1.0.0\"\n  },\n" +
				"  \"jsr\": {\n    \"@a/b@1.0.0\": {\n      \"integrity\": \"x\",\n      \"dependencies\": [\n" +
				"        \"jsr:@a/c\"\n      ]\n    },\n    \"@a/c@1.0.0\": {\n      \"integrity\": \"y\"\n    }\n  }\n}\n",
		},
		"a jsr dependency keeps its requirement when its name has several specifiers": {
			`{"specifiers":{"jsr:@a/b@^1":"1.0.0","jsr:@a/c@^1":"1.0.0","jsr:@a/c@^2":"2.0.0"},"jsr":{` +
				`"@a/b@1.0.0":{"integrity":"x","dependencies":["jsr:@a/c@^2","jsr:@a/c@^1"]}}}`,
			fiveComma + "  \"specifiers\": {\n    \"jsr:@a/b@1\": \"1.0.0\",\n    \"jsr:@a/c@1\": \"1.0.0\",\n" +
				"    \"jsr:@a/c@2\": \"2.0.0\"\n  },\n" +
				"  \"jsr\": {\n    \"@a/b@1.0.0\": {\n      \"integrity\": \"x\",\n      \"dependencies\": [\n" +
				"        \"jsr:@a/c@1\",\n        \"jsr:@a/c@2\"\n      ]\n    }\n  }\n}\n",
		},
		"a name used in both registries counts as several specifiers": {
			`{"specifiers":{"jsr:a@^1":"1.0.0","npm:a@^1":"1.0.0"},"jsr":{"a@1.0.0":{"integrity":"x","dependencies":["npm:a@^1"]}}}`,
			fiveComma + "  \"specifiers\": {\n    \"jsr:a@1\": \"1.0.0\",\n    \"npm:a@1\": \"1.0.0\"\n  },\n" +
				"  \"jsr\": {\n    \"a@1.0.0\": {\n      \"integrity\": \"x\",\n      \"dependencies\": [\n" +
				"        \"npm:a@1\"\n      ]\n    }\n  }\n}\n",
		},
		"npm dependencies are bare, versioned or aliased": {
			`{"npm":{"b@1.0.0":{"integrity":"i","dependencies":["a@2.0.0"]},` +
				`"a@2.0.0":{"dependencies":["b","c@npm:b@1.0.0"],"optionalPeers":["b@1.0.0"],"optionalDependencies":["a@1.0.0"]},` +
				`"a@1.0.0":{}}}`,
			fiveComma + "  \"npm\": {\n    \"a@1.0.0\": {},\n    \"a@2.0.0\": {\n      \"dependencies\": [\n" +
				"        \"b\",\n        \"c@npm:b@1.0.0\"\n      ],\n      \"optionalDependencies\": [\n        \"a@1.0.0\"\n      ],\n" +
				"      \"optionalPeers\": [\n        \"b\"\n      ]\n    },\n    \"b@1.0.0\": {\n      \"integrity\": \"i\",\n" +
				"      \"dependencies\": [\n        \"a@2.0.0\"\n      ]\n    }\n  }\n}\n",
		},
		"npm dependencies are sorted by the name they use, not by the printed text": {
			`{"npm":{"z@1.0.0":{},"y@1.0.0":{},"a@1.0.0":{"dependencies":["y","w@npm:z@1.0.0"]}}}`,
			fiveComma + "  \"npm\": {\n    \"a@1.0.0\": {\n      \"dependencies\": [\n        \"w@npm:z@1.0.0\",\n        \"y\"\n      ]\n    },\n" +
				"    \"y@1.0.0\": {},\n    \"z@1.0.0\": {}\n  }\n}\n",
		},
		"npm peer suffixes stay in the ids": {
			`{"npm":{"react-dom@18.0.0_react@18.0.0":{"dependencies":["react@18.0.0"]},"react@18.0.0":{}}}`,
			fiveComma + "  \"npm\": {\n    \"react-dom@18.0.0_react@18.0.0\": {\n      \"dependencies\": [\n        \"react\"\n      ]\n    },\n" +
				"    \"react@18.0.0\": {}\n  }\n}\n",
		},
		"npm os and cpu are on one line and the flags follow them": {
			`{"npm":{"p@1.0.0":{"integrity":"i","tarball":"t","bin":true,"scripts":true,"deprecated":true,` +
				`"cpu":["x64","arm64"],"os":["linux","darwin"],"scripts2":1}}}`,
			fiveComma + "  \"npm\": {\n    \"p@1.0.0\": {\n      \"integrity\": \"i\",\n      \"os\": [\"linux\", \"darwin\"],\n" +
				"      \"cpu\": [\"x64\", \"arm64\"],\n      \"deprecated\": true,\n      \"scripts\": true,\n      \"bin\": true,\n" +
				"      \"tarball\": \"t\"\n    }\n  }\n}\n",
		},
		"npm false flags, empty arrays and null are dropped": {
			`{"npm":{"p@1.0.0":{"integrity":null,"deprecated":false,"scripts":false,"bin":false,"os":[],"cpu":[],"dependencies":[]}}}`,
			fiveComma + "  \"npm\": {\n    \"p@1.0.0\": {}\n  }\n}\n",
		},
		"an empty integrity is kept": {
			`{"npm":{"p@1.0.0":{"integrity":""}}}`,
			fiveComma + "  \"npm\": {\n    \"p@1.0.0\": {\n      \"integrity\": \"\"\n    }\n  }\n}\n",
		},
		"strings are escaped like serde_json": {
			"{\"remote\":{\"k\":\"<a href=\\\"x\\\">&</a> \\\\ / \\u0001\\u001f\\b\\f\\n\\r\\t \\u007f \\u00e9 \\u2028 \\ud83d\\ude00\"}}",
			fiveComma + "  \"remote\": {\n    \"k\": \"<a href=\\\"x\\\">&</a> \\\\ / \\u0001\\u001f\\b\\f\\n\\r\\t \u007f é   \U0001F600\"\n  }\n}\n",
		},
		"keys are escaped too and sorted by their raw bytes": {
			`{"redirects":{"a\"b":"c","a\nb":"d"}}`,
			fiveComma + "  \"redirects\": {\n    \"a\\nb\": \"d\",\n    \"a\\\"b\": \"c\"\n  }\n}\n",
		},
		"map keys are sorted bytewise": {
			`{"remote":{"b":"1","B":"2","a":"3","é":"4","~":"5","@":"6"}}`,
			fiveComma + "  \"remote\": {\n    \"@\": \"6\",\n    \"B\": \"2\",\n    \"a\": \"3\",\n    \"b\": \"1\",\n" +
				"    \"~\": \"5\",\n    \"é\": \"4\"\n  }\n}\n",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := parseOrFatal(t, tt.in).Print()
			if got != tt.want {
				t.Fatalf("got\n%s\nwant\n%s", got, tt.want)
			}
			assertFixedPoint(t, got)
		})
	}
}

// TestPrintWorkspace covers the workspace section: the flattened root, members, links, and the overrides.
func TestPrintWorkspace(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		in   string
		want string
	}{
		"dependencies are normalised, sorted and de-duplicated": {
			`{"workspace":{"dependencies":["npm:b@^1.0.0","jsr:@a/c@1","jsr:@a/c@^1.0.0","jsr:@a/c@^1.0.0"],` +
				`"packageJson":{"dependencies":["npm:z","npm:a@~1.2.3"]}}}`,
			fiveComma + "  \"workspace\": {\n    \"dependencies\": [\n      \"jsr:@a/c@1\",\n      \"npm:b@1\"\n    ],\n" +
				"    \"packageJson\": {\n      \"dependencies\": [\n        \"npm:a@~1.2.3\",\n        \"npm:z@*\"\n      ]\n    }\n  }\n}\n",
		},
		"members keep empty entries and are sorted by path": {
			`{"workspace":{"members":{"packages/b":{},"packages/a":{"dependencies":["jsr:x@1"],"packageJson":{}}}}}`,
			fiveComma + "  \"workspace\": {\n    \"members\": {\n      \"packages/a\": {\n        \"dependencies\": [\n" +
				"          \"jsr:x@1\"\n        ]\n      },\n      \"packages/b\": {}\n    }\n  }\n}\n",
		},
		"links with every field": {
			`{"workspace":{"links":{"npm:b@1.0.0":{"peerDependenciesMeta":{"z":{"optional":true},"a":{"x":[1,{"y":null}],"optional":false}},` +
				`"peerDependencies":["npm:p@^1"],"optionalDependencies":["npm:o@2"],"dependencies":["npm:d@3","jsr:d@3"]},` +
				`"jsr:a@1.0.0":{"dependencies":[]}}}}`,
			fiveComma + "  \"workspace\": {\n    \"links\": {\n      \"jsr:a@1.0.0\": {},\n      \"npm:b@1.0.0\": {\n" +
				"        \"dependencies\": [\n          \"jsr:d@3\",\n          \"npm:d@3\"\n        ],\n" +
				"        \"optionalDependencies\": [\n          \"npm:o@2\"\n        ],\n" +
				"        \"peerDependencies\": [\n          \"npm:p@1\"\n        ],\n" +
				"        \"peerDependenciesMeta\": {\n          \"a\": {\n            \"x\": [\n              1,\n              {\n" +
				"                \"y\": null\n              }\n            ],\n            \"optional\": false\n          },\n" +
				"          \"z\": {\n            \"optional\": true\n          }\n        }\n      }\n    }\n  }\n}\n",
		},
		"patches is the old name of links": {
			`{"workspace":{"patches":{"jsr:a@1.0.0":{}}}}`,
			fiveComma + "  \"workspace\": {\n    \"links\": {\n      \"jsr:a@1.0.0\": {}\n    }\n  }\n}\n",
		},
		"link keys are written as they are": {
			`{"workspace":{"links":{"jsr:a@^1":{}}}}`,
			fiveComma + "  \"workspace\": {\n    \"links\": {\n      \"jsr:a@^1\": {}\n    }\n  }\n}\n",
		},
		"overrides of the root package.json": {
			`{"workspace":{"packageJson":{"overrides":{"foo":{".":"1.5.0","bar":"2.0.0"},"baz":"1"}}}}`,
			fiveComma + "  \"workspace\": {\n    \"packageJson\": {\n      \"overrides\": {\n        \"foo\": {\n" +
				"          \".\": \"1.5.0\",\n          \"bar\": \"2.0.0\"\n        },\n        \"baz\": \"1\"\n      }\n    }\n  }\n}\n",
		},
		"npmOverrides wins over packageJson.overrides and is written in packageJson": {
			`{"workspace":{"npmOverrides":{"a":"1"},"packageJson":{"dependencies":["npm:x@1"],"overrides":{"b":"2"}}}}`,
			fiveComma + "  \"workspace\": {\n    \"packageJson\": {\n      \"dependencies\": [\n        \"npm:x@1\"\n      ],\n" +
				"      \"overrides\": {\n        \"a\": \"1\"\n      }\n    }\n  }\n}\n",
		},
		"empty overrides are kept": {
			`{"workspace":{"packageJson":{"overrides":{}}}}`,
			fiveComma + "  \"workspace\": {\n    \"packageJson\": {\n      \"overrides\": {}\n    }\n  }\n}\n",
		},
		"null overrides are none": {
			`{"workspace":{"packageJson":{"overrides":null}}}`,
			onlyFive,
		},
		"member overrides are dropped": {
			`{"workspace":{"members":{"m":{"packageJson":{"overrides":{"a":"1"}}}}}}`,
			fiveComma + "  \"workspace\": {\n    \"members\": {\n      \"m\": {}\n    }\n  }\n}\n",
		},
		"overrides with scalars": {
			`{"workspace":{"packageJson":{"overrides":{"a":[true,false,null,"s",1,-2,1.5,1e3,18446744073709551615]}}}}`,
			fiveComma + "  \"workspace\": {\n    \"packageJson\": {\n      \"overrides\": {\n        \"a\": [\n          true,\n          false,\n" +
				"          null,\n          \"s\",\n          1,\n          -2,\n          1.5,\n          1000.0,\n" +
				"          18446744073709551615\n        ]\n      }\n    }\n  }\n}\n",
		},
		"os and cpu arrays inside free-form JSON are written inline too": {
			`{"workspace":{"packageJson":{"overrides":{"os":["a","b"],"x":{"cpu":["c"],"os":[]},"y":["d"]}}}}`,
			fiveComma + "  \"workspace\": {\n    \"packageJson\": {\n      \"overrides\": {\n        \"os\": [\"a\", \"b\"],\n" +
				"        \"x\": {\n          \"cpu\": [\"c\"],\n          \"os\": []\n        },\n        \"y\": [\n          \"d\"\n        ]\n      }\n    }\n  }\n}\n",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := parseOrFatal(t, tt.in).Print()
			if got != tt.want {
				t.Fatalf("got\n%s\nwant\n%s", got, tt.want)
			}
			assertFixedPoint(t, got)
		})
	}
}

// TestPrintSorting checks the orders of the printer: PackageNv for jsr, the text for the rest.
func TestPrintSorting(t *testing.T) {
	t.Parallel()
	keysOf := func(t *testing.T, c *Content, section string) []string {
		t.Helper()
		root, err := ParseJSON([]byte(c.Print()))
		if err != nil {
			t.Fatal(err)
		}
		v, _ := root.Get(section)
		var keys []string
		for _, m := range v.Obj {
			keys = append(keys, m.Key)
		}
		return keys
	}
	tests := map[string]struct {
		section string
		in      string
		want    string
	}{
		"jsr orders versions by precedence, not by text": {
			sectionJSR,
			`{"jsr":{"a@10.0.0":{"integrity":"i"},"a@2.0.0":{"integrity":"i"},"a@2.0.0-rc.10":{"integrity":"i"},` +
				`"a@2.0.0-rc.2":{"integrity":"i"},"a@2.0.0-alpha":{"integrity":"i"},"a@2.0.0-1":{"integrity":"i"},"a@1.9.9":{"integrity":"i"}}}`,
			"a@1.9.9 a@2.0.0-1 a@2.0.0-alpha a@2.0.0-rc.2 a@2.0.0-rc.10 a@2.0.0 a@10.0.0",
		},
		"jsr orders names first, bytewise": {
			sectionJSR,
			`{"jsr":{"b@1.0.0":{"integrity":"i"},"@z/a@9.0.0":{"integrity":"i"},"@a/b@1.0.0":{"integrity":"i"},` +
				`"a@1.0.0":{"integrity":"i"},"@a/b-c@1.0.0":{"integrity":"i"},"@a/b@2.0.0":{"integrity":"i"}}}`,
			"@a/b@1.0.0 @a/b@2.0.0 @a/b-c@1.0.0 @z/a@9.0.0 a@1.0.0 b@1.0.0",
		},
		"jsr merges versions that differ by build metadata only": {
			sectionJSR,
			`{"jsr":{"a@1.0.0+b":{"integrity":"second"},"a@1.0.0+a":{"integrity":"first"},"a@1.0.1":{"integrity":"i"}}}`,
			"a@1.0.0+b a@1.0.1",
		},
		"npm is ordered by the text of the id": {
			"npm",
			`{"npm":{"a@10.0.0":{},"a@2.0.0":{},"@s/n@1.0.0":{},"a-b@1.0.0":{},"a@2.0.0_p@1":{}}}`,
			"@s/n@1.0.0 a-b@1.0.0 a@10.0.0 a@2.0.0 a@2.0.0_p@1",
		},
		"specifiers are ordered by the normalised text": {
			"specifiers",
			`{"specifiers":{"npm:a@*":"1.0.0","jsr:a@^1.0.0":"1.0.0","jsr:a@1.5.0":"1.5.0","jsr:a@*":"2.0.0",` +
				`"jsr:a@^1.5.0":"1.5.0","jsr:a@~1.0.0":"1.0.0","jsr:b@1":"1.0.0","jsr:@a/c@1":"1.0.0","jsr:a@latest":"1.0.0"}}`,
			"jsr:@a/c@1 jsr:a@* jsr:a@1 jsr:a@1.0 jsr:a@1.5.0 jsr:a@^1.5.0 jsr:a@latest jsr:b@1 npm:a@*",
		},
		"caret and the short form are one requirement": {
			"specifiers",
			`{"specifiers":{"jsr:a@^1.0.0":"1.0.0","jsr:a@1":"1.1.0","jsr:a@^0.1.0":"0.1.0","jsr:a@0.1":"0.1.1"}}`,
			"jsr:a@0.1 jsr:a@1",
		},
		"workspace members and links are ordered by key": {
			"workspace",
			`{"workspace":{"members":{"b":{},"a":{}},"links":{"npm:b@1":{},"jsr:z@1":{},"jsr:a@1":{}},"dependencies":["jsr:a@1"]}}`,
			"dependencies members links",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := parseOrFatal(t, tt.in)
			if got := strings.Join(keysOf(t, c, tt.section), " "); got != tt.want {
				t.Fatalf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}

// TestPrintLockfileTests ports the v5 printing tests of deno_lockfile's src/lib.rs (lockfile_with_redirects,
// test_insert_redirect and test_insert_jsr). They build the content through the Lockfile API there; here the
// content is read from the equivalent version 5 text, since the model has no mutating Lockfile.
func TestPrintLockfileTests(t *testing.T) {
	t.Parallel()
	t.Run("lockfile_with_redirects", func(t *testing.T) {
		t.Parallel()
		c := parseOrFatal(
			t,
			`{"version":"5","redirects":{"https://deno.land/x/std/mod.ts":"https://deno.land/std@0.190.0/mod.ts"}}`,
		)
		c.Redirects["https://deno.land/x/other/mod.ts"] = "https://deno.land/x/other@0.1.0/mod.ts"
		want := `{
  "version": "5",
  "redirects": {
    "https://deno.land/x/other/mod.ts": "https://deno.land/x/other@0.1.0/mod.ts",
    "https://deno.land/x/std/mod.ts": "https://deno.land/std@0.190.0/mod.ts"
  }
}
`
		if got := c.Print(); got != want {
			t.Fatalf("got\n%s", got)
		}
	})
	t.Run("test_insert_redirect", func(t *testing.T) {
		t.Parallel()
		c := parseOrFatal(
			t,
			`{"version":"5","redirects":{"https://deno.land/x/std/mod.ts":"https://deno.land/std@0.190.0/mod.ts"}}`,
		)
		c.Redirects["https://deno.land/x/std/mod.ts"] = "https://deno.land/std@0.190.1/mod.ts"
		c.Redirects["https://deno.land/x/std/other.ts"] = "https://deno.land/std@0.190.1/other.ts"
		want := `{
  "version": "5",
  "redirects": {
    "https://deno.land/x/std/mod.ts": "https://deno.land/std@0.190.1/mod.ts",
    "https://deno.land/x/std/other.ts": "https://deno.land/std@0.190.1/other.ts"
  }
}
`
		if got := c.Print(); got != want {
			t.Fatalf("got\n%s", got)
		}
	})
	t.Run("test_insert_jsr", func(t *testing.T) {
		t.Parallel()
		c := parseOrFatal(t, `{"version":"5","specifiers":{"jsr:path":"jsr:@std/path@0.75.0"}}`)
		c.Specifiers[0].Resolved = "jsr:@std/path@0.75.1"
		req, err := denosemver.ParseJsrDepPackageReq("jsr:@foo/bar@^2")
		if err != nil {
			t.Fatal(err)
		}
		c.Specifiers = append(c.Specifiers, Specifier{Req: req, Resolved: "jsr:@foo/bar@2.1.2"})
		want := `{
  "version": "5",
  "specifiers": {
    "jsr:@foo/bar@2": "jsr:@foo/bar@2.1.2",
    "jsr:path@*": "jsr:@std/path@0.75.1"
  }
}
`
		if got := c.Print(); got != want {
			t.Fatalf("got\n%s", got)
		}
	})
}

// TestEditModel edits a lockfile through the model the way a rewrite for a new version of a package does.
func TestEditModel(t *testing.T) {
	t.Parallel()
	data := "testdata/knope/workspaces/in/deno.lock"
	src := readFile(t, data)
	c := parseOrFatal(t, src)

	mustReq := func(text string) denosemver.JsrDepPackageReq {
		req, err := denosemver.ParseJsrDepPackageReq(text)
		if err != nil {
			t.Fatal(err)
		}
		return req
	}
	// set the resolved version of the specifiers of @scope/first-package
	for i := range c.Specifiers {
		if c.Specifiers[i].Req.Req.Name == "@scope/first-package" {
			c.Specifiers[i].Resolved = "1.1.0"
		}
	}
	// rename the registry key
	for i := range c.JSR {
		if c.JSR[i].Nv.Name == "@scope/first-package" {
			v, err := denosemver.ParseStandard("1.1.0")
			if err != nil {
				t.Fatal(err)
			}
			c.JSR[i].Nv.Version = v
		}
	}
	// the workspace lists: replace the requirement, move the link
	ws := &c.Workspace
	ws.Root.Dependencies[0] = mustReq("jsr:@scope/first-package@1.1.0")
	ws.Root.PackageJSONDependencies[0] = mustReq("jsr:@scope/first-package@1.1.0")
	member := ws.Members["second"]
	member.Dependencies[0] = mustReq("jsr:@scope/first-package@1.1.0")
	member.PackageJSONDependencies[0] = mustReq("jsr:@scope/first-package@1.1.0")
	ws.Links["jsr:@scope/first-package@1.1.0"] = ws.Links["jsr:@scope/first-package@1.0.0"]
	delete(ws.Links, "jsr:@scope/first-package@1.0.0")

	got := c.Print()
	if !strings.Contains(got, `"jsr:@scope/first-package@^1.0.0": "1.1.0"`) &&
		!strings.Contains(got, `"jsr:@scope/first-package@1": "1.1.0"`) {
		t.Fatalf("specifier not updated:\n%s", got)
	}
	for _, want := range []string{`"@scope/first-package@1.1.0": {`, `"jsr:@scope/first-package@1.1.0": {`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in\n%s", want, got)
		}
	}
	if strings.Contains(got, "1.0.0") {
		t.Errorf("the old version is still there:\n%s", got)
	}
	assertFixedPoint(t, got)
}

func TestPrintDoesNotChangeTheModel(t *testing.T) {
	t.Parallel()
	c := parseOrFatal(t, readFile(t, "testdata/knope/workspaces/in/deno.lock"))
	first := c.Print()
	c.Specifiers[0], c.Specifiers[1] = c.Specifiers[1], c.Specifiers[0] // order in the slice does not matter
	if second := c.Print(); second != first {
		t.Fatalf("Print depends on the order of the slices:\n%s\n%s", first, second)
	}
}
