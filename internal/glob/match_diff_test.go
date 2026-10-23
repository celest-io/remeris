package glob

import (
	"context"
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

// oldMatch is the backtracking matcher that matchTokens replaced. It is
// exponential in the number of stars, but its answers define the semantics.
func oldMatch(tokens []token, s []rune) bool {
	for ti, t := range tokens {
		if t.kind == tokSeq || t.kind == tokRecursive {
			for i := 0; i <= len(s); i++ {
				if t.kind == tokRecursive && i > 0 && i < len(s) && s[i-1] != '/' {
					continue
				}
				if oldMatch(tokens[ti+1:], s[i:]) {
					return true
				}
			}
			return false
		}
		if len(s) == 0 || !t.matchesRune(s[0]) {
			return false
		}
		s = s[1:]
	}
	return len(s) == 0
}

var (
	diffPieces = []string{
		"a",
		"b",
		"c",
		"?",
		"*",
		"[a-c]",
		"[!a]",
		"[b]",
		recursive,
		recursiveDir,
		"/",
		".",
		"*",
		"a*",
		"[a-b]*",
		"é",
		"日",
		"]",
		"-",
		`\`,
		"A",
		"!",
		"[!é]",
		"[日-é]",
		"[]]",
		"[a-]",
		"[!]]",
		"[A-Z]",
	}
	diffNames = []rune(`abcé日]-\AB!/.`)
)

func randomPattern(r *rand.Rand) string {
	var b strings.Builder
	for range 1 + r.IntN(7) {
		b.WriteString(diffPieces[r.IntN(len(diffPieces))])
	}
	return b.String()
}

func randomName(r *rand.Rand) string {
	out := make([]rune, r.IntN(11))
	for i := range out {
		out[i] = diffNames[r.IntN(len(diffNames))]
	}
	return string(out)
}

// TestMatchDifferential compares the polynomial matcher with the old one on random
// small patterns and names.
func TestMatchDifferential(t *testing.T) {
	t.Parallel()
	const seed = 20260101
	r := rand.New(rand.NewPCG(seed, seed)) //nolint:gosec // a fixed seed keeps the differential test reproducible
	compiled, cases, positive := 0, 0, 0
	for compiled < 6000 {
		p, err := compile(randomPattern(r))
		if err != nil {
			continue
		}
		compiled++
		for range 20 {
			name := randomName(r)
			cases++
			if matchTokens(p.tokens, []rune(name)) {
				positive++
			}
			if got, want := matchTokens(p.tokens, []rune(name)), oldMatch(p.tokens, []rune(name)); got != want {
				t.Fatalf("seed %d: pattern %q name %q: new %v, old %v", seed, p.src, name, got, want)
			}
		}
	}
	if cases < 20000 {
		t.Fatalf("only %d cases", cases)
	}
	t.Logf("%d patterns, %d cases, %d matching", compiled, cases, positive)
}

func FuzzMatchDifferential(f *testing.F) {
	for _, seed := range [][2]string{{"a*b", "aab"}, {"**/a", "x/a"}, {"*[!a]*?", "bbb"}, {"a/**", "a/b/"}} {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, pat, name string) {
		if len(pat) > 12 || len([]rune(name)) > 14 {
			t.Skip("keep the exponential reference matcher fast")
		}
		p, err := compile(pat)
		if err != nil {
			t.Skip("invalid pattern")
		}
		if got, want := matchTokens(p.tokens, []rune(name)), oldMatch(p.tokens, []rune(name)); got != want {
			t.Fatalf("pattern %q name %q: new %v, old %v", pat, name, got, want)
		}
	})
}

// TestMatchPathological checks that hostile patterns finish quickly.
func TestMatchPathological(t *testing.T) {
	t.Parallel()
	a255 := strings.Repeat("a", 255)
	tests := map[string]struct {
		pattern string
		name    string
		want    bool
	}{
		"ten stars then b":     {strings.Repeat("*a", 10) + "*b", a255, false},
		"twelve stars then b":  {strings.Repeat("*a", 12) + "*b", a255, false},
		"stars and any":        {strings.Repeat("*?", 30) + "b", a255, false},
		"stars match":          {strings.Repeat("*a", 12) + "*", a255, true},
		"classes with stars":   {strings.Repeat("*[a-c]", 12) + "*[!a]", a255, false},
		"recursive mix":        {"**/" + strings.Repeat("*a", 12) + "*b", strings.Repeat("a/", 120), false},
		"recursive mix nested": {strings.Repeat("**/*a", 1) + strings.Repeat("*a", 10) + "*b", a255, false},
		"literal tail":         {strings.Repeat("*a", 12) + "*" + strings.Repeat("a", 20) + "b", a255, false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p, err := compile(tc.pattern)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			done := make(chan bool, 1)
			start := time.Now()
			go func() { done <- p.matches(tc.name) }()
			select {
			case got := <-done:
				if got != tc.want {
					t.Errorf("match = %v, want %v", got, tc.want)
				}
				if d := time.Since(start); d > 100*time.Millisecond {
					t.Errorf("took %v, want under 100ms", d)
				}
			case <-ctx.Done():
				t.Fatal("did not finish within 5s")
			}
		})
	}
}
