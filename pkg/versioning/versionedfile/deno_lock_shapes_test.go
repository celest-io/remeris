package versionedfile_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

const (
	denoOptional = "optionalDependencies"
	denoPeer     = "peerDependencies"
	denoCouldNot = "Could not update deno.lock: "
	denoInvalid  = denoCouldNot + "Invalid "
	denoV150     = "1.5.0"
	denoTP       = "@t/p"
)

func denoJSRLock(jsr string) string {
	return `{"version":"5","specifiers":{"jsr:@t/p@^1":"1.0.0"},"jsr":{` + jsr + `}}`
}

// jsrIntegrities returns the integrity of every jsr entry whose key starts with prefix.
func jsrIntegrities(t *testing.T, out, prefix string) (integrities []string, total int) {
	t.Helper()
	m, _ := jsonAt(t, out, "jsr").(map[string]any)
	for k, v := range m {
		if strings.HasPrefix(k, prefix) {
			integrities = append(integrities, v.(map[string]any)["integrity"].(string))
		}
	}
	return integrities, len(m)
}

func TestDenoLockRegistryCollisions(t *testing.T) {
	t.Parallel()
	// The renamed entry is inserted last, so it replaces an existing entry of the new key, in either key order.
	for name, jsr := range map[string]string{
		"collision old first": `"@t/p@1.0.0":{"integrity":"old"},"@t/p@1.1.0":{"integrity":"existing"}`,
		"collision new first": `"@t/p@1.1.0":{"integrity":"existing"},"@t/p@1.0.0":{"integrity":"old"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, _ := lockWrite(t, setDeno(t, denoJSRLock(jsr), [2]string{denoTP, v110}))
			if got, total := jsrIntegrities(t, out, "@t/p@1.1.0"); total != 1 || !reflect.DeepEqual(got, []string{"old"}) {
				t.Errorf("got %v of %d entries, want the renamed one only:\n%s", got, total, out)
			}
		})
	}
}

func TestDenoLockSwapRemove(t *testing.T) {
	t.Parallel()
	// Keys that differ only by build metadata are one key of the model, and the last one in the object wins.
	// serde_json removes with a swap: taking "@t/p@1.0.0" out of [old, y, z] leaves [z, y], so y survives. A
	// removal that keeps the order would leave z.
	const jsr = `"@t/p@1.0.0":{"integrity":"old"},"@t/p@2.0.0+y":{"integrity":"y"},"@t/p@2.0.0+z":{"integrity":"z"}`
	out, _ := lockWrite(t, setDeno(t, denoJSRLock(jsr), [2]string{denoTP, denoV150}))
	if got, _ := jsrIntegrities(t, out, "@t/p@2.0.0"); !reflect.DeepEqual(got, []string{"y"}) {
		t.Errorf("got %v, want [y]:\n%s", got, out)
	}
	if got, _ := jsrIntegrities(t, out, "@t/p@"+denoV150); !reflect.DeepEqual(got, []string{"old"}) {
		t.Errorf("renamed entry %v:\n%s", got, out)
	}
}

func TestDenoLockUnmatchedLinkStaysUnchanged(t *testing.T) {
	t.Parallel()
	// A link whose own key names another package is not touched, even if its lists contain the dependency.
	const content = `{"version":"5",
 "specifiers":{"jsr:@t/p@^1":"1.0.0","jsr:@t/q@^1":"1.0.0"},
 "jsr":{"@t/p@1.0.0":{"integrity":"p"},"@t/q@1.0.0":{"integrity":"q"}},
 "workspace":{"links":{"jsr:@t/q@1.0.0":{"dependencies":["jsr:@t/p@1.0.0"],
  "optionalDependencies":["jsr:@t/p@1.0.0"],"peerDependencies":["jsr:@t/p@1.0.0"]}}}}`
	out, _ := lockWrite(t, setDeno(t, content, [2]string{denoTP, denoV150}))
	for _, list := range []string{denoDeps, denoOptional, denoPeer} {
		if got := jsonAt(
			t,
			out,
			denoWorkspace,
			"links",
			"jsr:@t/q@1.0.0",
			list,
		); !reflect.DeepEqual(
			got,
			strs("jsr:@t/p@1.0.0"),
		) {
			t.Errorf("%s of the unmatched link = %v\n%s", list, got, out)
		}
	}
}

func TestDenoLockTwoCallsAreTheCombinedEdit(t *testing.T) {
	t.Parallel()
	steps := [][2]string{{denoFirst, denoLockV120}, {denoLeftPad, denoV140}}
	both, diff := lockWrite(t, setDeno(t, denoSample, steps...))
	reversed, _ := lockWrite(t, setDeno(t, denoSample, steps[1], steps[0]))
	if both != reversed {
		t.Errorf("order matters for independent dependencies:\n%s\n---\n%s", both, reversed)
	}
	// The second call can as well be made on the text the first one wrote.
	first, _ := lockWrite(t, setDeno(t, denoSample, steps[0]))
	second, _ := lockWrite(t, setDeno(t, first, steps[1]))
	if second != both || diff != "@scope/first = 1.2.0, left-pad = 1.4.0" {
		t.Errorf("two files differ from one:\n%s\n---\n%s\n%q", second, both, diff)
	}
}

func TestDenoLockMalformedShapes(t *testing.T) {
	t.Parallel()
	const spec = `"specifiers":{"jsr:@t/p@^1":"1.0.0","npm:é@^1":"1.0.0"}`
	const head = `{"version":"5",` + spec
	tests := map[string]struct {
		content string
		dep     string
		wantErr string
		wantKey string
	}{
		"shape workspace array": {head + `,"workspace":[]}`, denoTP, denoInvalid + "workspace section", ""},
		"shape member array":    {head + `,"workspace":{"members":{"m":[]}}}`, denoTP, denoInvalid + "workspace section", ""},
		"shape link array": {
			head + `,"workspace":{"links":{"jsr:@t/p@1.0.0":[]}}}`,
			denoTP,
			denoInvalid + "workspace section",
			"",
		},
		"shape jsr array":      {head + `,"jsr":[]}`, denoTP, denoInvalid + "jsr section", ""},
		"shape specifiers arr": {`{"version":"5","specifiers":[]}`, denoTP, "", ""},
		"shape npm multibyte":  {head + `,"npm":{"é@1.0.0":{"integrity":"e"}}}`, "é", "", "é@1.1.0"},
		"shape npm bad id":     {head + `,"npm":{"éé":{"integrity":"e"}}}`, "é", denoCouldNot, ""},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, err := newDenoLock(tc.content)
			if err != nil {
				t.Fatal(err)
			}
			f, err = f.SetVersion(mustVersion(t, v110), &tc.dep, versionedfile.Standard)
			switch {
			case tc.wantErr != "":
				if err == nil || !strings.HasPrefix(err.Error(), tc.wantErr) {
					t.Errorf("got %v, want an error starting %q", err, tc.wantErr)
				}
			case err != nil:
				t.Errorf("unexpected error %v", err)
			case tc.wantKey == "":
				if f.Write() != nil {
					t.Error("want no write")
				}
			default:
				out, _ := lockWrite(t, f)
				if jsonAt(t, out, "npm", tc.wantKey) == nil ||
					jsonAt(t, out, "specifiers", "npm:é@1") != v110 {
					t.Errorf("npm %s not renamed:\n%s", tc.wantKey, out)
				}
			}
		})
	}
}
