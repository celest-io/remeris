package denosemver

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

// Version is a deno_semver Version: a Semantic Version parsed with npm's loose grammar.
//
// It is not pkg/versioning/semver.Version on purpose. The two grammars differ: deno_semver accepts leading zeros
// ("01.02.03"), a leading "=" and "v", surrounding white space and a pre-release without the dash ("1.2.3beta"),
// and it compares a numeric pre-release identifier that has a leading zero as a number. Converting through
// String() and semver.Parse is only possible for the versions Remeris writes itself.
type Version struct {
	Major, Minor, Patch uint64
	// Pre holds the pre-release identifiers; Build holds the build metadata identifiers.
	Pre, Build []string
}

// String prints major.minor.patch[-pre][+build], like the Display of the Rust type.
func (v Version) String() string {
	var b strings.Builder
	b.WriteString(strconv.FormatUint(v.Major, 10))
	b.WriteByte('.')
	b.WriteString(strconv.FormatUint(v.Minor, 10))
	b.WriteByte('.')
	b.WriteString(strconv.FormatUint(v.Patch, 10))
	if len(v.Pre) > 0 {
		b.WriteByte('-')
		b.WriteString(strings.Join(v.Pre, "."))
	}
	if len(v.Build) > 0 {
		b.WriteByte('+')
		b.WriteString(strings.Join(v.Build, "."))
	}
	return b.String()
}

// Equal reports whether the versions are identical, build metadata included (the derived PartialEq of Rust).
// Compare ignores the build metadata.
func (v Version) Equal(o Version) bool {
	return v.Major == o.Major && v.Minor == o.Minor && v.Patch == o.Patch &&
		slices.Equal(v.Pre, o.Pre) && slices.Equal(v.Build, o.Build)
}

// Compare returns -1, 0 or 1 with the precedence of the Rust Ord implementation, which follows node-semver:
// the build metadata is ignored, a pre-release sorts before the release, and pre-release identifiers that parse
// as u64 sort before the others and numerically among themselves. Two numeric identifiers that are equal as
// numbers (such as "01" and "1") then fall back to a plain string comparison.
func (v Version) Compare(o Version) int {
	if c := cmp.Or(cmp.Compare(v.Major, o.Major), cmp.Compare(v.Minor, o.Minor), cmp.Compare(v.Patch, o.Patch)); c != 0 {
		return c
	}
	switch {
	case len(v.Pre) == 0 && len(o.Pre) == 0:
		return 0
	case len(v.Pre) > 0 && len(o.Pre) == 0:
		return -1
	case len(v.Pre) == 0 && len(o.Pre) > 0:
		return 1
	}
	for i := 0; ; i++ {
		if i >= len(v.Pre) && i >= len(o.Pre) {
			return 0
		}
		if i >= len(v.Pre) {
			return -1
		}
		if i >= len(o.Pre) {
			return 1
		}
		a, b := v.Pre[i], o.Pre[i]
		an, aIsNum := parseU64(a)
		bn, bIsNum := parseU64(b)
		switch {
		case aIsNum && bIsNum:
			if c := cmp.Compare(an, bn); c != 0 {
				return c
			}
		case aIsNum:
			return -1
		case bIsNum:
			return 1
		}
		if c := strings.Compare(a, b); c != 0 {
			return c
		}
	}
}

// parseU64 is Rust's str::parse::<u64> for the identifiers the grammar produces (ASCII letters, digits, '-').
func parseU64(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(s, 10, 64)
	return n, err == nil
}

// ParseStandard parses a version the way deno_semver's Version::parse_standard does: with the loose npm
// grammar. On failure the error is a *ParseError with the text "Invalid version" whose source is a *Failure.
func ParseStandard(text string) (Version, error) {
	v, err := parseNpmVersion(text)
	if err != nil {
		return Version{}, &ParseError{msg: "Invalid version", src: err.src}
	}
	return v, nil
}

// ParseNpm parses a version the way Version::parse_from_npm does. On failure the error is a *ParseError with
// the text "Invalid npm version" whose source is a *Failure.
func ParseNpm(text string) (Version, error) {
	v, err := parseNpmVersion(text)
	if err != nil {
		return Version{}, err
	}
	return v, nil
}

func parseNpmVersion(text string) (Version, *ParseError) {
	text = strings.TrimSpace(text)
	v, f := withFailureHandling(npmVersion)(text)
	if f != nil {
		return Version{}, &ParseError{msg: "Invalid npm version", src: f}
	}
	return v, nil
}

func npmVersion(in string) (string, Version, *parseErr) {
	in, _, err := maybe(ch('='))(in)
	if err != nil {
		return in, Version{}, err
	}
	in, _, _ = skipWhitespace(in)
	in, _, err = maybe(ch('v'))(in)
	if err != nil {
		return in, Version{}, err
	}
	in, _, _ = skipWhitespace(in)
	in, major, err := looseNr(in)
	if err != nil {
		return in, Version{}, err
	}
	in, _, err = ch('.')(in)
	if err != nil {
		return in, Version{}, err
	}
	in, minor, err := looseNr(in)
	if err != nil {
		return in, Version{}, err
	}
	in, _, err = ch('.')(in)
	if err != nil {
		return in, Version{}, err
	}
	in, patch, err := looseNr(in)
	if err != nil {
		return in, Version{}, err
	}
	in, q, err := maybe(qualifier)(in)
	if err != nil {
		return in, Version{}, err
	}
	return in, Version{Major: major, Minor: minor, Patch: patch, Pre: q.val.pre, Build: q.val.build}, nil
}
