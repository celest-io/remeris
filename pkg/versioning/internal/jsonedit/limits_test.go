package jsonedit_test

import (
	"strings"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/internal/jsonedit"
)

func TestDepthLimit(t *testing.T) {
	t.Parallel()
	nest := func(depth int, open, closing, inner string) string {
		return strings.Repeat(open, depth) + inner + strings.Repeat(closing, depth)
	}
	tests := map[string]struct {
		src     string
		wantErr bool
	}{
		"127 arrays":         {nest(127, "[", "]", ""), false},
		"128 arrays":         {nest(128, "[", "]", ""), true},
		"127 objects":        {nest(127, `{"a":`, "}", "1"), false},
		"128 objects":        {nest(128, `{"a":`, "}", "1"), true},
		"127 arrays, scalar": {nest(127, "[", "]", "1"), false},
		"128 arrays, scalar": {nest(128, "[", "]", "1"), true},
		"127 mixed":          {nest(63, `[{"a":`, "}]", "[]"), false},
		"128 mixed":          {nest(64, `[{"a":`, "}]", "1"), true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := jsonedit.Parse([]byte(tc.src))
			if (err != nil) != tc.wantErr {
				t.Errorf("Parse error = %v, want error: %v", err, tc.wantErr)
			}
		})
	}
}

func TestNilValuesAreNull(t *testing.T) {
	t.Parallel()
	obj := jsonedit.NewObject(jsonedit.Member{Name: "a"})
	if !obj.Set("b", nil) {
		t.Fatal("Set on an object reported false")
	}
	obj.Set("c", jsonedit.NewArray(nil, jsonedit.NewInt(1)))
	const want = "{\n  \"a\": null,\n  \"b\": null,\n  \"c\": [\n    null,\n    1\n  ]\n}"
	if got := string(jsonedit.Marshal(obj)); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestSetOnNonObject(t *testing.T) {
	t.Parallel()
	if jsonedit.NewArray().Set("a", jsonedit.NewNull()) {
		t.Error("Set on an array reported true")
	}
}

func TestStringReplacesInvalidUTF8(t *testing.T) {
	t.Parallel()
	got, _ := jsonedit.String("a\xff\xfeb").Str()
	if got != "a�b" {
		t.Errorf("Str = %q", got)
	}
}
