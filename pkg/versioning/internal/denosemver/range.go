package denosemver

import (
	"math"
	"slices"
	"strconv"
	"strings"
)

// Ported from deno_semver 0.9.1, src/range.rs (MIT licence, Copyright 2018-2025 the Deno authors): the range
// types, the conversion of a partial version to a range, the satisfaction check and the normalised printer.
// Arithmetic on version numbers wraps around on overflow, like a release build of the Rust crate.

type boundKind int

const (
	inclusive boundKind = iota
	exclusive
)

type versionBound struct {
	kind    boundKind
	version Version
}

// rangeBound is either a version bound or unbounded (matches everything).
type rangeBound struct {
	unbounded bool
	bound     versionBound
}

func unbounded() rangeBound { return rangeBound{unbounded: true} }

func boundOf(kind boundKind, v Version) rangeBound {
	return rangeBound{bound: versionBound{kind: kind, version: v}}
}

func (b rangeBound) equal(o rangeBound) bool {
	if b.unbounded || o.unbounded {
		return b.unbounded == o.unbounded
	}
	return b.bound.kind == o.bound.kind && b.bound.version.Equal(o.bound.version)
}

func (b rangeBound) clampStart(other rangeBound) rangeBound {
	if b.unbounded {
		return other
	}
	if other.unbounded {
		return b
	}
	switch b.bound.version.Compare(other.bound.version) {
	case 1:
		return b
	case -1:
		return other
	}
	if b.bound.kind == exclusive {
		return b
	}
	return other
}

func (b rangeBound) clampEnd(other rangeBound) rangeBound {
	if b.unbounded {
		return other
	}
	if other.unbounded {
		return b
	}
	switch b.bound.version.Compare(other.bound.version) {
	case 1:
		return other
	case -1:
		return b
	}
	if b.bound.kind == exclusive {
		return b
	}
	return other
}

func (b rangeBound) hasPreWithExactMajorMinorPatch(v Version) bool {
	return !b.unbounded && len(b.bound.version.Pre) > 0 && b.bound.version.Major == v.Major &&
		b.bound.version.Minor == v.Minor && b.bound.version.Patch == v.Patch
}

type versionRange struct {
	start, end rangeBound
}

func allRange() versionRange { return versionRange{start: unbounded(), end: unbounded()} }

// noneRange is the range that matches nothing: [0.0.0, 0.0.0).
func noneRange() versionRange {
	return versionRange{start: boundOf(inclusive, Version{}), end: boundOf(exclusive, Version{})}
}

func (r versionRange) equal(o versionRange) bool { return r.start.equal(o.start) && r.end.equal(o.end) }

// isNone reports whether the range matches nothing: an exclusive end at 0.0.0.
func (r versionRange) isNone() bool {
	return !r.end.unbounded && r.end.bound.kind == exclusive &&
		r.end.bound.version.Major == 0 && r.end.bound.version.Minor == 0 && r.end.bound.version.Patch == 0
}

func (r versionRange) satisfies(v Version) bool {
	ok := r.minSatisfies(v) && r.maxSatisfies(v)
	if ok && len(v.Pre) > 0 {
		return r.start.hasPreWithExactMajorMinorPatch(v) || r.end.hasPreWithExactMajorMinorPatch(v)
	}
	return ok
}

func (r versionRange) minSatisfies(v Version) bool {
	if r.start.unbounded {
		return true
	}
	switch v.Compare(r.start.bound.version) {
	case -1:
		return false
	case 0:
		return r.start.bound.kind == inclusive
	}
	return true
}

func (r versionRange) maxSatisfies(v Version) bool {
	if r.end.unbounded {
		return true
	}
	switch v.Compare(r.end.bound.version) {
	case -1:
		return true
	case 0:
		return r.end.bound.kind == inclusive
	}
	return false
}

// clamp intersects the range with another one, the way the "range" rule combines its "simple" parts.
func (r versionRange) clamp(o versionRange) versionRange {
	start := r.start.clampStart(o.start)
	end := r.end.clampEnd(o.end)
	if !start.unbounded {
		// clamp the start range to the end when greater
		start = start.clampEnd(end)
	}
	return versionRange{start: start, end: end}
}

// String prints the range in the normalised form: ^1.0.0 becomes "1", ^0.1.0 becomes "0.1", ~1.2.3 stays, and
// the rest is ">=a <b".
func (r versionRange) String() string {
	var b strings.Builder
	r.write(&b)
	return b.String()
}

func (r versionRange) write(b *strings.Builder) {
	switch {
	case r.start.unbounded && r.end.unbounded:
		b.WriteByte('*')
	case r.start.unbounded:
		if r.end.bound.kind == inclusive {
			b.WriteString("<=")
		} else {
			b.WriteByte('<')
		}
		b.WriteString(r.end.bound.version.String())
	case r.end.unbounded:
		if r.start.bound.kind == inclusive {
			b.WriteString(">=")
		} else {
			b.WriteByte('>')
		}
		b.WriteString(r.start.bound.version.String())
	default:
		writeBounded(b, r.start.bound, r.end.bound)
	}
}

func writeBounded(b *strings.Builder, start, end versionBound) {
	switch {
	case start.kind == inclusive && end.kind == inclusive:
		switch {
		case start.version.Equal(end.version):
			b.WriteString(start.version.String())
		case isZeroVersion(start.version):
			b.WriteString("<=" + end.version.String())
		default:
			b.WriteString(">=" + start.version.String() + " <=" + end.version.String())
		}
	case start.kind == inclusive:
		if writeShortHand(b, start.version, end.version) {
			return
		}
		switch {
		case matchesTilde(start.version, end.version):
			b.WriteString("~" + start.version.String())
		case matchesCaret(start.version, end.version):
			b.WriteString("^" + start.version.String())
		case isZeroVersion(start.version):
			b.WriteString("<" + end.version.String())
		default:
			b.WriteString(">=" + start.version.String() + " <" + end.version.String())
		}
	case end.kind == inclusive:
		b.WriteString(">" + start.version.String() + " <=" + end.version.String())
	default:
		b.WriteString(">" + start.version.String() + " <" + end.version.String())
	}
}

// writeShortHand prints [start, end) as "1" (for ^1.0.0) or "1.1" (for ~1.1.0) when it can.
func writeShortHand(b *strings.Builder, start, end Version) bool {
	if end.Patch != 0 || start.Patch != 0 || len(end.Pre) > 0 || len(end.Build) > 0 || len(start.Pre) > 0 ||
		len(start.Build) > 0 {
		return false
	}
	if end.Minor == 0 && start.Minor == 0 {
		if start.Major != math.MaxUint64 && end.Major == start.Major+1 {
			b.WriteString(uitoa(start.Major))
			return true
		}
	} else if start.Major == end.Major && start.Minor != math.MaxUint64 && end.Minor == start.Minor+1 {
		b.WriteString(uitoa(start.Major) + "." + uitoa(start.Minor))
		return true
	}
	return false
}

func matchesTilde(start, end Version) bool {
	if len(end.Build) > 0 || len(end.Pre) > 0 {
		return false
	}
	if start.Major != end.Major {
		return false
	}
	if start.Minor == end.Minor {
		return end.Patch == start.Patch && len(start.Pre) > 0
	}
	if start.Minor == math.MaxUint64 {
		return false
	}
	// ~0.1.2 is equivalent to >=0.1.2 <0.2.0 and ~1.2.3 to >=1.2.3 <1.3.0
	return end.Minor == start.Minor+1 && end.Patch == 0
}

func matchesCaret(start, end Version) bool {
	if len(end.Build) > 0 || len(end.Pre) > 0 {
		return false
	}
	if start.Major == 0 {
		// ~0.0.x is different from ^0.0.x, so handle that
		if start.Minor == 0 && end.Minor == 0 {
			return start.Patch != math.MaxUint64 && end.Patch == start.Patch+1
		}
		return false // it always normalises to a tilde in this case
	}
	if start.Major == math.MaxUint64 {
		return false
	}
	// ^1.2.3 is equivalent to >=1.2.3 <2.0.0
	return end.Major == start.Major+1 && end.Minor == 0 && end.Patch == 0
}

func isZeroVersion(v Version) bool {
	return v.Major == 0 && v.Minor == 0 && v.Patch == 0 && len(v.Pre) == 0 && len(v.Build) == 0
}

// rangeSet is a union of ranges (VersionRangeSet).
type rangeSet []versionRange

func (s rangeSet) satisfies(v Version) bool {
	return slices.ContainsFunc(s, func(r versionRange) bool { return r.satisfies(v) })
}

func (s rangeSet) equal(o rangeSet) bool { return slices.EqualFunc(s, o, versionRange.equal) }

// String joins the ranges with " || "; an empty set prints "*".
func (s rangeSet) String() string {
	if len(s) == 0 {
		return "*"
	}
	parts := make([]string, len(s))
	for i, r := range s {
		parts[i] = r.String()
	}
	return strings.Join(parts, " || ")
}

// xRange is a number or a wildcard (x, X or *).
type xRange struct {
	wildcard bool
	val      uint64
}

func xVal(v uint64) xRange { return xRange{val: v} }

var xWildcard = xRange{wildcard: true}

// partial is a version with optional parts, such as "1", "1.x" or "1.2.3-beta".
type partial struct {
	major, minor, patch xRange
	pre, build          []string
}

func (p partial) asTildeVersionRange() versionRange {
	// tilde ranges allow patch-level changes
	if p.major.wildcard {
		return allRange()
	}
	var end Version
	if p.minor.wildcard {
		end = Version{Major: p.major.val + 1}
	} else {
		end = Version{Major: p.major.val, Minor: p.minor.val + 1}
	}
	return versionRange{start: p.asLowerBound(), end: boundOf(exclusive, end)}
}

func (p partial) asCaretVersionRange() versionRange {
	// caret ranges allow patch and minor updates, except when the leading parts are < 1: then it only bumps the
	// first non-zero part or the patch
	if p.major.wildcard {
		return allRange()
	}
	nextMajor := Version{Major: p.major.val + 1}
	var end Version
	switch {
	case p.major.val > 0, p.minor.wildcard:
		end = nextMajor
	default:
		nextMinor := Version{Minor: p.minor.val + 1}
		switch {
		case p.minor.val > 0, p.patch.wildcard:
			end = nextMinor
		default:
			end = Version{Patch: p.patch.val + 1}
		}
	}
	return versionRange{start: p.asLowerBound(), end: boundOf(exclusive, end)}
}

func (p partial) asLowerBound() rangeBound {
	return boundOf(inclusive, Version{
		Major: valOrZero(p.major),
		Minor: valOrZero(p.minor),
		Patch: valOrZero(p.patch),
		Pre:   slices.Clone(p.pre),
		Build: slices.Clone(p.build),
	})
}

func valOrZero(x xRange) uint64 {
	if x.wildcard {
		return 0
	}
	return x.val
}

func (p partial) asUpperBound() rangeBound {
	var end Version
	kind := inclusive
	if p.patch.wildcard {
		end.Minor++
		kind = exclusive
	} else {
		end.Patch = p.patch.val
	}
	if p.minor.wildcard {
		end.Minor = 0
		end.Major++
		kind = exclusive
	} else {
		end.Minor += p.minor.val
	}
	if p.major.wildcard {
		return unbounded()
	}
	end.Major += p.major.val
	if kind == inclusive {
		end.Pre = slices.Clone(p.pre)
	}
	return boundOf(kind, end)
}

func (p partial) asEqualRange() versionRange {
	if p.major.wildcard || p.minor.wildcard || p.patch.wildcard {
		return p.asGreaterRange(inclusive)
	}
	v := Version{
		Major: p.major.val,
		Minor: p.minor.val,
		Patch: p.patch.val,
		Pre:   slices.Clone(p.pre),
		Build: slices.Clone(p.build),
	}
	return versionRange{start: boundOf(inclusive, v), end: boundOf(inclusive, v)}
}

func (p partial) asGreaterThan(startKind boundKind) versionRange {
	if p.major.wildcard {
		if startKind == inclusive {
			return allRange()
		}
		return noneRange()
	}
	var start Version
	if startKind == inclusive {
		start.Pre = slices.Clone(p.pre)
	}
	start.Major = p.major.val
	if p.minor.wildcard {
		if startKind == exclusive {
			startKind = inclusive
			start.Major++
		}
	} else {
		start.Minor = p.minor.val
	}
	if p.patch.wildcard {
		if startKind == exclusive {
			startKind = inclusive
			start.Minor++
		}
	} else {
		start.Patch = p.patch.val
		if len(p.pre) > 0 && startKind == exclusive {
			start.Pre = slices.Clone(p.pre)
		}
	}
	return versionRange{start: boundOf(startKind, start), end: unbounded()}
}

func (p partial) asLessThan(endKind boundKind) versionRange {
	if p.major.wildcard {
		if endKind == inclusive {
			return allRange()
		}
		return noneRange()
	}
	end := Version{Major: p.major.val}
	if p.minor.wildcard {
		if endKind == inclusive {
			end.Major++
		}
		endKind = exclusive
	} else {
		end.Minor = p.minor.val
	}
	if p.patch.wildcard {
		if endKind == inclusive {
			end.Minor++
		}
		endKind = exclusive
	} else {
		end.Patch = p.patch.val
		if len(p.pre) > 0 && endKind == exclusive {
			end.Pre = slices.Clone(p.pre)
		}
	}
	if endKind == inclusive {
		end.Pre = slices.Clone(p.pre)
	}
	// the start does not include the 0.0.0 pre-release versions
	return versionRange{start: boundOf(inclusive, Version{}), end: boundOf(endKind, end)}
}

func (p partial) asGreaterRange(startKind boundKind) versionRange {
	if p.major.wildcard {
		return allRange()
	}
	start := Version{Major: p.major.val}
	end := Version{Major: p.major.val}
	if p.patch.wildcard {
		if !p.minor.wildcard {
			end.Minor++
		}
	} else {
		start.Patch = p.patch.val
		end.Patch = p.patch.val
	}
	if p.minor.wildcard {
		end.Major++
	} else {
		start.Minor = p.minor.val
		end.Minor += p.minor.val
	}
	endKind := exclusive
	if startKind == inclusive && start.Equal(end) {
		endKind = inclusive
	}
	return versionRange{start: boundOf(startKind, start), end: boundOf(endKind, end)}
}

func uitoa(n uint64) string { return strconv.FormatUint(n, 10) }
