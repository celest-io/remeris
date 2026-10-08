package command

import (
	"slices"
	"testing"
)

const (
	cmdEcho = "echo"
)

func TestSplit(t *testing.T) {
	cases := map[string]struct {
		line string
		want []string
	}{
		"empty":            {"", nil},
		"only spaces":      {"  \t ", nil},
		"words":            {"git commit -m", []string{"git", "commit", "-m"}},
		"extra whitespace": {"  a   b\tc\n", []string{"a", "b", "c"}},
		"double quotes": {
			`git commit -m "chore: prepare release 1.2.3"`,
			[]string{"git", "commit", "-m", "chore: prepare release 1.2.3"},
		},
		"single quotes":               {`echo 'a  b'`, []string{cmdEcho, "a  b"}},
		"other quote inside quotes":   {`echo "it's" 'say "hi"'`, []string{cmdEcho, "it's", `say "hi"`}},
		"escaped space":               {`a\ b c`, []string{"a b", "c"}},
		"escaped quote":               {`echo \"hi\"`, []string{cmdEcho, `"hi"`}},
		"escaped quote inside quotes": {`echo "a \"b\" c"`, []string{cmdEcho, `a "b" c`}},
		"escaped backslash":           {`a\\b`, []string{`a\b`}},
		"dollar is literal": {
			`echo $HOME ${USER} "$PATH" '$x'`,
			[]string{cmdEcho, "$HOME", "${USER}", "$PATH", "$x"},
		},
		"adjacent quoted parts join": {`'123''456' a"b c"d`, []string{"123456", "ab cd"}},
		"empty quotes are a word":    {`a "" b ''`, []string{"a", "", "b", ""}},
		"unterminated quote":         {`echo "abc def`, []string{cmdEcho, "abc def"}},
		"trailing backslash dropped": {`echo a\`, []string{cmdEcho, "a"}},
		"unicode":                    {`echo héllo "wörld ✓"`, []string{cmdEcho, "héllo", "wörld ✓"}},
		"tab inside quotes":          {"echo 'a\tb'", []string{cmdEcho, "a\tb"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := Split(tc.line); !slices.Equal(got, tc.want) {
				t.Errorf("Split(%q) = %q, want %q", tc.line, got, tc.want)
			}
		})
	}
}
