package jsonedit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/internal/jsonedit"
)

// TestSerdeNumbers compares against the output of serde_json 1.0.151 (preserve_order, without float_roundtrip
// and arbitrary_precision) for serde_json::from_str::<Value> followed by to_string. The golden file was generated
// with a Rust harness: it holds hand-picked edge cases and random inputs.
func TestSerdeNumbers(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("testdata", "serde_numbers.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.Lines(string(data)) {
		line = strings.TrimRight(line, "\n")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		in, want, ok := strings.Cut(line, "\t")
		if !ok {
			t.Fatalf("bad golden line %q", line)
		}
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			v, err := jsonedit.Parse([]byte(in))
			if want == "ERR" {
				if err == nil {
					t.Fatalf("Parse(%s) succeeded, want an error", in)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%s): %v", in, err)
			}
			if got := string(jsonedit.Marshal(v)); got != want {
				t.Errorf("Parse(%s) prints %s, want %s", in, got, want)
			}
		})
	}
}

func FuzzNumbers(f *testing.F) {
	for _, s := range []string{"0", "-0", "1e400", "123.456e-7", "18446744073709551616", "1e-99999999999", "0.1e2147483648"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		// Only numbers are of interest: anything else must still not panic.
		v, err := jsonedit.Parse([]byte(s))
		if err != nil || v.Kind() != jsonedit.KindNumber {
			return
		}
		out := string(jsonedit.Marshal(v))
		if strings.ContainsAny(out, " \n") {
			t.Errorf("number %q printed as %q", s, out)
		}
		// The printed form is valid JSON. (It need not be a fixed point: serde_json's parser is not exact.)
		if _, err := jsonedit.Parse([]byte(out)); err != nil {
			t.Fatalf("output %q of %q does not parse: %v", out, s, err)
		}
	})
}
