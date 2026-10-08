package conventional

import "unicode"

// EqualFold reports whether a and b are equal under Unicode full case folding, which is how the unicase crate
// compares the commit types and footer tokens in Knope. It differs from [strings.EqualFold], which only uses
// simple folding: here "ﬁx" equals "fix" and "ß" equals "ss".
func EqualFold(a, b string) bool {
	fa, fb := fold(a), fold(b)
	if len(fa) != len(fb) {
		return false
	}
	for i := range fa {
		if fa[i] != fb[i] {
			return false
		}
	}
	return true
}

// fold expands full foldings and maps every rune to the smallest member of its simple folding orbit.
func fold(s string) []rune {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if expansion, ok := fullFold[r]; ok {
			for _, e := range expansion {
				out = append(out, orbitMin(e))
			}
			continue
		}
		out = append(out, orbitMin(r))
	}
	return out
}

func orbitMin(r rune) rune {
	smallest := r
	for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
		smallest = min(smallest, f)
	}
	return smallest
}
