package denosemver

import (
	"strconv"
	"strings"
	"unicode"
)

// Ported from deno_semver 0.9.1 (MIT licence, Copyright 2018-2025 the Deno authors): src/common.rs, the parsers
// of src/npm.rs (loose, node-semver compatible), src/specifier.rs (strict, for specifiers in code and
// lockfiles) and src/range_set_or_tag.rs (the normalised form that PackageReq serialises to).

// qual is the qualifier of a version: its pre-release and build identifiers.
type qual struct {
	pre, build []string
}

// isAlphanumeric is Rust's char::is_alphanumeric: Alphabetic or Numeric.
func isAlphanumeric(c rune) bool {
	return unicode.IsLetter(c) || unicode.IsNumber(c) || unicode.Is(unicode.Other_Alphabetic, c) ||
		unicode.Is(unicode.Other_Lowercase, c) || unicode.Is(unicode.Other_Uppercase, c)
}

func isASCIIAlphanumeric(c rune) bool {
	return isASCIIDigit(c) || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// isValidNpmTag follows npm: a tag is anything that is not URL encoded.
func isValidNpmTag(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	for _, c := range value {
		if !isAlphanumeric(c) && c != '-' && c != '_' && c != '.' && c != '~' {
			return false
		}
	}
	return true
}

// logical-or ::= ( ' ' ) * '||' ( ' ' ) *
func logicalOr(in string) (string, string, *parseErr) {
	return delimited(skipWhitespace, tag("||"), skipWhitespace)(in)
}

// logical-and ::= ( ' ' ) * '&&' ( ' ' ) *
func logicalAnd(in string) (string, string, *parseErr) {
	return delimited(skipWhitespace, tag("&&"), skipWhitespace)(in)
}

// part ::= nr | [-0-9A-Za-z]+
func part(in string) (string, string, *parseErr) {
	return ifTrue(takeWhile(func(c rune) bool { return isASCIIAlphanumeric(c) || c == '-' }),
		func(s string) bool { return s != "" })(in)
}

// parts ::= part ( '.' part ) *
func parts(in string) (string, []string, *parseErr) {
	return ifTrue(separatedList(part, ch('.')), func(items []string) bool { return len(items) > 0 })(in)
}

// qualifier ::= ( '-' pre )? ( '+' build )?, where the dash is optional when loose is set.
func qualifierWith(loose bool) parser[qual] {
	pre := preceded(ch('-'), parts)
	if loose {
		pre = preceded(maybe(ch('-')), parts)
	}
	build := preceded(ch('+'), parts)
	return func(in string) (string, qual, *parseErr) {
		in, preParts, err := maybe(pre)(in)
		if err != nil {
			return in, qual{}, err
		}
		in, buildParts, err := maybe(build)(in)
		if err != nil {
			return in, qual{}, err
		}
		return in, qual{pre: preParts.val, build: buildParts.val}, nil
	}
}

// qualifier is the loose qualifier of the npm grammar (and of the version parser).
func qualifier(in string) (string, qual, *parseErr) { return qualifierWith(true)(in) }

// partialWith builds the "partial" rule: xr ( '.' xr ( '.' xr qualifier ? )? )?
func partialWith(xr parser[xRange], q parser[qual]) parser[partial] {
	return func(in string) (string, partial, *parseErr) {
		in, major, err := xr(in)
		if err != nil {
			return in, partial{}, err
		}
		in, minor, err := maybe(preceded(ch('.'), xr))(in)
		if err != nil {
			return in, partial{}, err
		}
		var patch option[xRange]
		if minor.ok {
			in, patch, err = maybe(preceded(ch('.'), xr))(in)
			if err != nil {
				return in, partial{}, err
			}
		}
		var qu option[qual]
		if patch.ok {
			in, qu, err = maybe(q)(in)
			if err != nil {
				return in, partial{}, err
			}
		}
		p := partial{major: major, minor: xWildcard, patch: xWildcard, pre: qu.val.pre, build: qu.val.build}
		if minor.ok {
			p.minor = minor.val
		}
		if patch.ok {
			p.patch = patch.val
		}
		return in, p, nil
	}
}

type primitiveKind int

const (
	kindGreaterThan primitiveKind = iota
	kindLessThan
	kindGreaterThanOrEqual
	kindLessThanOrEqual
	kindEqual
)

func primitiveKindParser(in string) (string, primitiveKind, *parseErr) {
	return or(
		mapP(tag(">="), func(string) primitiveKind { return kindGreaterThanOrEqual }),
		mapP(tag("<="), func(string) primitiveKind { return kindLessThanOrEqual }),
		mapP(ch('<'), func(rune) primitiveKind { return kindLessThan }),
		mapP(ch('>'), func(rune) primitiveKind { return kindGreaterThan }),
		mapP(ch('='), func(rune) primitiveKind { return kindEqual }),
	)(in)
}

// primitive ::= ( '<' | '>' | '>=' | '<=' | '=' ) partial, as a range.
func primitiveRange(p parser[partial]) parser[versionRange] {
	return func(in string) (string, versionRange, *parseErr) {
		in, kind, err := primitiveKindParser(in)
		if err != nil {
			return in, versionRange{}, err
		}
		in, _, err = skipWhitespace(in)
		if err != nil {
			return in, versionRange{}, err
		}
		in, pt, err := p(in)
		if err != nil {
			return in, versionRange{}, err
		}
		switch kind {
		case kindEqual:
			return in, pt.asEqualRange(), nil
		case kindGreaterThan:
			return in, pt.asGreaterThan(exclusive), nil
		case kindGreaterThanOrEqual:
			return in, pt.asGreaterThan(inclusive), nil
		case kindLessThan:
			return in, pt.asLessThan(exclusive), nil
		default:
			return in, pt.asLessThan(inclusive), nil
		}
	}
}

// looseNr is the "nr" rule of the loose grammars: any run of digits, so that 01.02.03 parses.
func looseNr(in string) (string, uint64, *parseErr) {
	rest, digits, err := ifTrue(substring(skipWhile(isASCIIDigit)), func(s string) bool { return s != "" })(in)
	if err != nil {
		return in, 0, err
	}
	return parseNr(rest, digits)
}

// strictNr is "nr ::= '0' | ['1'-'9'] ( ['0'-'9'] ) *".
func strictNr(in string) (string, uint64, *parseErr) {
	rest, _, err := tag("0")(in)
	if err == nil {
		return rest, 0, nil
	}
	nonZeroDigit := ifTrue(nextChar, func(c rune) bool { return isASCIIDigit(c) && c != '0' })
	rest, digits, err := ifTrue(
		substring(pair(nonZeroDigit, skipWhile(isASCIIDigit))),
		func(s string) bool { return s != "" },
	)(
		in,
	)
	if err != nil {
		return in, 0, err
	}
	return parseNr(rest, digits)
}

func parseNr(rest, digits string) (string, uint64, *parseErr) {
	n, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return rest, 0, fail(rest, "Error parsing '"+digits+"' to u64.\n\nnumber too large to fit in target type")
	}
	return rest, n, nil
}

func wildcardOrVal(wild parser[string], nr parser[uint64]) parser[xRange] {
	return or(mapP(wild, func(string) xRange { return xWildcard }), mapP(nr, xVal))
}

// rangeOrInvalid is a range, or the text of something that did not parse as one and the reason why.
type rangeOrInvalid struct {
	rng     versionRange
	invalid bool
	failure *parseErr
	text    string
}

// parseRangeOrInvalid parses a range that must end the input or be followed by "||". Anything else becomes an
// invalid range that extends to the next "||".
func parseRangeOrInvalid(parseRange parser[versionRange]) parser[rangeOrInvalid] {
	return func(in string) (string, rangeOrInvalid, *parseErr) {
		rest, r, err := parseRange(in)
		if err == nil {
			if rest == "" || strings.HasPrefix(strings.TrimLeftFunc(rest, unicode.IsSpace), "||") {
				return rest, rangeOrInvalid{rng: r}, nil
			}
			err = errBacktrace
		}
		end := strings.Index(in, "||")
		if end < 0 {
			end = len(in)
		}
		return in[end:], rangeOrInvalid{invalid: true, failure: err, text: strings.TrimSpace(in[:end])}, nil
	}
}

// rangeSetOrTag is the parsed form of a version requirement: a union of ranges, or a tag such as "latest".
type rangeSetOrTag struct {
	isTag bool
	tag   string
	set   rangeSet
}

func (r rangeSetOrTag) String() string {
	if r.isTag {
		return r.tag
	}
	return r.set.String()
}

func (r rangeSetOrTag) equal(o rangeSetOrTag) bool {
	if r.isTag || o.isTag {
		return r.isTag == o.isTag && r.tag == o.tag
	}
	return r.set.equal(o.set)
}

func allSet() rangeSetOrTag { return rangeSetOrTag{set: rangeSet{allRange()}} }

// parseRangeSet is the shared body of the "range-set" rule. With strictInvalid set (the normalised grammar), an
// invalid range is an error; otherwise (npm) it is dropped.
func parseRangeSet(parseRange parser[versionRange], strictInvalid bool) parser[rangeSetOrTag] {
	return func(in string) (string, rangeSetOrTag, *parseErr) {
		if in == "" {
			return in, allSet(), nil
		}
		in, ranges, err := separatedList(parseRangeOrInvalid(parseRange), logicalOr)(in)
		if err != nil {
			return in, rangeSetOrTag{}, err
		}
		if len(ranges) == 1 && ranges[0].invalid {
			if isValidNpmTag(ranges[0].text) {
				return in, rangeSetOrTag{isTag: true, tag: ranges[0].text}, nil
			}
			return in, rangeSetOrTag{}, ranges[0].failure
		}
		set := make(rangeSet, 0, len(ranges))
		for _, r := range ranges {
			if r.invalid {
				if strictInvalid {
					return in, rangeSetOrTag{}, r.failure
				}
				continue
			}
			if !r.rng.isNone() {
				set = append(set, r.rng)
			}
		}
		return in, rangeSetOrTag{set: set}, nil
	}
}

// ---- npm grammar (loose) ----
//
// range-set  ::= range ( logical-or range ) *
// range      ::= hyphen | simple ( ' ' simple ) * | ''
// hyphen     ::= partial ' - ' partial
// simple     ::= primitive | partial | tilde | caret
// primitive  ::= ( '<' | '>' | '>=' | '<=' | '=' ) partial
// partial    ::= xr ( '.' xr ( '.' xr qualifier ? )? )?
// xr         ::= 'x' | 'X' | '*' | nr
// tilde      ::= '~' partial
// caret      ::= '^' partial

var npmXr = wildcardOrVal(or(tag("x"), tag("X"), tag("*")), looseNr)

func npmPartial(in string) (string, partial, *parseErr) {
	in, _, err := maybe(ch('v'))(in) // skip leading v
	if err != nil {
		return in, partial{}, err
	}
	return partialWith(npmXr, qualifier)(in)
}

type hyphen struct{ start, end partial }

func npmHyphen(in string) (string, hyphen, *parseErr) {
	in, first, err := npmPartial(in)
	if err != nil {
		return in, hyphen{}, err
	}
	in, _, err = whitespace(in)
	if err != nil {
		return in, hyphen{}, err
	}
	in, _, err = tag("-")(in)
	if err != nil {
		return in, hyphen{}, err
	}
	in, _, err = whitespace(in)
	if err != nil {
		return in, hyphen{}, err
	}
	in, second, err := npmPartial(in)
	if err != nil {
		return in, hyphen{}, err
	}
	return in, hyphen{start: first, end: second}, nil
}

func skipWhitespaceOrV(in string) (string, struct{}, *parseErr) {
	in, _, _ = skipWhitespace(in)
	in, _, err := maybe(ch('v'))(in)
	if err != nil {
		return in, struct{}{}, err
	}
	in, _, _ = skipWhitespace(in)
	return in, struct{}{}, nil
}

// operator parses a tilde or a caret, with an optional primitive kind before it ("<= ^1.2.3": node-semver
// accepts it when white space separates the two).
func operator(raw parser[struct{}]) parser[struct{}] {
	return or(preceded(terminated(primitiveKindParser, whitespace), raw), raw)
}

func rawTilde(in string) (string, struct{}, *parseErr) {
	head := terminated(or(tag("~>"), tag("~")), skipWhile(func(c rune) bool { return c == '=' }))
	return mapP(pair(head, skipWhitespaceOrV), func(tuple[string, struct{}]) struct{} { return struct{}{} })(in)
}

func rawCaret(in string) (string, struct{}, *parseErr) {
	head := terminated(tag("^"), skipWhile(func(c rune) bool { return c == '=' }))
	return mapP(pair(head, skipWhitespaceOrV), func(tuple[string, struct{}]) struct{} { return struct{}{} })(in)
}

func npmSimple(in string) (string, versionRange, *parseErr) {
	return or(
		mapP(preceded(operator(rawTilde), npmPartial), partial.asTildeVersionRange),
		mapP(preceded(operator(rawCaret), npmPartial), partial.asCaretVersionRange),
		primitiveRange(npmPartial),
		mapP(npmPartial, partial.asEqualRange),
	)(in)
}

func rangeSeparator(in string) (string, struct{}, *parseErr) {
	comma := delimited(skipWhitespace, ch(','), skipWhitespace)
	return or(
		mapP(logicalAnd, func(string) struct{} { return struct{}{} }),
		mapP(comma, func(rune) struct{} { return struct{}{} }),
		mapP(whitespace, func(string) struct{} { return struct{}{} }),
	)(in)
}

func clampAll(ranges []versionRange) versionRange {
	final := allRange()
	for _, r := range ranges {
		final = final.clamp(r)
	}
	return final
}

func npmRange(in string) (string, versionRange, *parseErr) {
	return or(
		mapP(npmHyphen, func(h hyphen) versionRange {
			return versionRange{start: h.start.asLowerBound(), end: h.end.asUpperBound()}
		}),
		mapP(separatedList(npmSimple, rangeSeparator), clampAll),
	)(in)
}

// ---- normalised grammar (what PackageReq prints) ----
//
// The same rules as npm, but without hyphen ranges, "&&", commas, "x" and "X", and with a "*" only.

var normalizedXr = wildcardOrVal(tag("*"), looseNr)

func normalizedPartial(in string) (string, partial, *parseErr) {
	return partialWith(normalizedXr, qualifier)(in)
}

func normalizedSimple(in string) (string, versionRange, *parseErr) {
	return or(
		mapP(preceded(ch('~'), normalizedPartial), partial.asTildeVersionRange),
		mapP(preceded(ch('^'), normalizedPartial), partial.asCaretVersionRange),
		primitiveRange(normalizedPartial),
		mapP(normalizedPartial, partial.asEqualRange),
	)(in)
}

func normalizedRange(in string) (string, versionRange, *parseErr) {
	return mapP(separatedList(normalizedSimple, whitespace), clampAll)(in)
}

// ---- specifier grammar (strict) ----
//
// version_range ::= partial | tilde | caret, with "x" | "X" | "*" | strict nr.

var specifierXr = wildcardOrVal(or(tag("x"), tag("X"), tag("*")), strictNr)

func specifierPartial(in string) (string, partial, *parseErr) {
	return partialWith(specifierXr, qualifierWith(false))(in)
}

func specifierRange(in string) (string, versionRange, *parseErr) {
	return or(
		mapP(preceded(ch('~'), specifierPartial), partial.asTildeVersionRange),
		mapP(preceded(ch('^'), specifierPartial), partial.asCaretVersionRange),
		mapP(specifierPartial, partial.asEqualRange),
	)(in)
}

// specifierReq parses a requirement from a specifier: one range, or a tag.
func specifierReq(in string) (string, rangeSetOrTag, *parseErr) {
	rest, r, err := specifierRange(in)
	if err == nil {
		return rest, rangeSetOrTag{set: rangeSet{r}}, nil
	}
	// like Rust, consume everything and decide whether the text is a tag
	switch {
	case isValidNpmTag(in):
		return "", rangeSetOrTag{isTag: true, tag: in}, nil
	case strings.TrimSpace(in) == "":
		return "", rangeSetOrTag{}, fail(in, "Empty version constraint.")
	}
	return "", rangeSetOrTag{}, err
}
