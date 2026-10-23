package denosemver

import "strings"

// VersionReq is a version requirement: a union of ranges such as "^1.2.3 || 2", or a tag such as "latest".
//
// It keeps the raw text it was parsed from. Knope matches on that text (VersionText), while the lockfile printer
// prints the normalised form (Normalized). Two requirements are equal when their parsed forms are equal, whatever
// their raw text (Equal), so "1" and "1.x" are equal.
type VersionReq struct {
	raw   string
	inner rangeSetOrTag
}

// WildcardVersionReq returns the requirement that matches any version, which has the raw text "*".
func WildcardVersionReq() VersionReq {
	return VersionReq{raw: "*", inner: rangeSetOrTag{set: rangeSet{allRange()}}}
}

// ParseVersionReqSpecifier parses a requirement with the strict grammar of specifiers: one tilde, caret or
// partial version, or a tag. The raw text is the input. On failure the error is a *ParseError with the text
// "Invalid specifier version requirement".
func ParseVersionReqSpecifier(text string) (VersionReq, error) {
	inner, f := withFailureHandling(specifierReq)(text)
	if f != nil {
		return VersionReq{}, &ParseError{msg: "Invalid specifier version requirement", src: f}
	}
	return VersionReq{raw: text, inner: inner}, nil
}

// ParseVersionReqLoose parses a requirement with the npm grammar (parse_from_npm): ranges, hyphen ranges, "||",
// "&&" and commas, "x" wildcards, a leading "v", and tags. The text is trimmed first, and the raw text is the
// trimmed text. On failure the error is a *ParseError with the text "Invalid version requirement".
func ParseVersionReqLoose(text string) (VersionReq, error) {
	text = strings.TrimSpace(text)
	inner, f := withFailureHandling(parseRangeSet(npmRange, false))(text)
	if f != nil {
		return VersionReq{}, &ParseError{msg: "Invalid version requirement", src: f}
	}
	return VersionReq{raw: text, inner: inner}, nil
}

// ParseVersionReqNormalized parses a requirement printed by Normalized (parse_from_normalized). An invalid
// range is an error here. On failure the error is a *ParseError with the text
// "Invalid normalized version requirement".
func ParseVersionReqNormalized(text string) (VersionReq, error) {
	inner, f := withFailureHandling(parseRangeSet(normalizedRange, true))(text)
	if f != nil {
		return VersionReq{}, &ParseError{msg: "Invalid normalized version requirement", src: f}
	}
	return VersionReq{raw: text, inner: inner}, nil
}

// VersionText returns the raw text the requirement was parsed from.
func (r VersionReq) VersionText() string { return r.raw }

// String returns the raw text, like the Display of the Rust type.
func (r VersionReq) String() string { return r.raw }

// Normalized prints the parsed form: ^1.0.0 becomes "1", ^0.1.0 becomes "0.1", an empty set becomes "*", a tag
// stays as it is, and several ranges are joined with " || ". This is the text deno.lock stores.
func (r VersionReq) Normalized() string { return r.inner.String() }

// Tag returns the tag and true when the requirement is a tag such as "latest".
func (r VersionReq) Tag() (string, bool) { return r.inner.tag, r.inner.isTag }

// Equal compares the parsed forms and ignores the raw text.
func (r VersionReq) Equal(o VersionReq) bool { return r.inner.equal(o.inner) }

// Matches reports whether the version satisfies the requirement. A tag matches nothing (the Rust method panics).
func (r VersionReq) Matches(v Version) bool {
	return !r.inner.isTag && r.inner.set.satisfies(v)
}
