// Package semver implements Semantic Versioning 2.0.0 versions: parsing, display and precedence (spec F1.1, D8).
package semver

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/celest-io/remeris/internal/diag"
)

// Version is a Semantic Versioning 2.0.0 version.
type Version struct {
	Major, Minor, Patch uint64
	// Pre holds the pre-release identifiers; empty means a stable version.
	Pre []PreID
	// Build holds the build metadata identifiers, kept for display and writing. Precedence ignores them.
	Build []string
}

// PreID is one dot-separated pre-release identifier. Str always holds the identifier text; Num is
// meaningful only when IsNum is set.
type PreID struct {
	Str   string
	Num   uint64
	IsNum bool
}

// ParseError is the error Parse returns. Match it with errors.As. It is a diag.Diagnostic with the message
// "Found invalid semantic version {detail}" and the help "The version must be a valid Semantic Version".
type ParseError struct {
	// Detail says what is wrong with the input.
	Detail string
	d      *diag.Error
}

func (e *ParseError) Error() string        { return e.d.Error() }
func (e *ParseError) Code() string         { return e.d.Code() }
func (e *ParseError) Help() string         { return e.d.Help() }
func (e *ParseError) URL() string          { return e.d.URL() }
func (e *ParseError) Labels() []diag.Label { return e.d.Labels() }
func (e *ParseError) SourceCode() string   { return e.d.SourceCode() }
func (e *ParseError) Related() []error     { return e.d.Related() }

var _ diag.Diagnostic = (*ParseError)(nil)

const helpText = "The version must be a valid Semantic Version"

func newParseError(detail string) *ParseError {
	return &ParseError{
		Detail: detail,
		d:      diag.New("Found invalid semantic version " + detail).WithCode("remeris::version").WithHelp(helpText),
	}
}

// Detail messages mirror the wording of the Rust integer parse errors that Knope reports.
const (
	detailEmpty    = "cannot parse integer from empty string"
	detailDigit    = "invalid digit found in string"
	detailOverflow = "number too large to fit in target type"
	detailParts    = "Version must have exactly 3 parts"
	detailLeading  = "numbers must not have leading zeros"
)

// Parse parses a full SemVer 2.0.0 version. It accepts no "v" prefix and no surrounding whitespace. On
// failure the error is a *ParseError.
func Parse(s string) (Version, error) {
	rest, build, hasBuild := strings.Cut(s, "+")
	core, pre, hasPre := strings.Cut(rest, "-")

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Version{}, newParseError(detailParts)
	}
	var nums [3]uint64
	for i, p := range parts {
		n, detail := parseNumeric(p)
		if detail != "" {
			return Version{}, newParseError(detail)
		}
		nums[i] = n
	}
	v := Version{Major: nums[0], Minor: nums[1], Patch: nums[2]}

	if hasPre {
		for id := range strings.SplitSeq(pre, ".") {
			p, detail := parsePreID(id)
			if detail != "" {
				return Version{}, newParseError(detail)
			}
			v.Pre = append(v.Pre, p)
		}
	}
	if hasBuild {
		for id := range strings.SplitSeq(build, ".") {
			if id == "" {
				return Version{}, newParseError(detailEmpty)
			}
			if !isIdentChars(id) {
				return Version{}, newParseError(detailDigit)
			}
			v.Build = append(v.Build, id)
		}
	}
	return v, nil
}

// parseNumeric parses digits without a leading zero. The detail is empty on success.
func parseNumeric(s string) (uint64, string) {
	if s == "" {
		return 0, detailEmpty
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return 0, detailDigit
		}
	}
	if len(s) > 1 && s[0] == '0' {
		return 0, detailLeading
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, detailOverflow
	}
	return n, ""
}

func parsePreID(s string) (PreID, string) {
	if s == "" {
		return PreID{}, detailEmpty
	}
	if !isIdentChars(s) {
		return PreID{}, detailDigit
	}
	if strings.Trim(s, "0123456789") != "" {
		return PreID{Str: s}, ""
	}
	n, detail := parseNumeric(s)
	if detail != "" {
		return PreID{}, detail
	}
	return PreID{Str: s, Num: n, IsNum: true}, ""
}

func isIdentChars(s string) bool {
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && c != '-' {
			return false
		}
	}
	return true
}

// MustParse is like Parse but panics on error. Use it for tests and constants.
func MustParse(s string) Version {
	v, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return v
}

// String returns the canonical form, including the pre-release and the build metadata.
func (v Version) String() string {
	var b strings.Builder
	b.WriteString(strconv.FormatUint(v.Major, 10))
	b.WriteByte('.')
	b.WriteString(strconv.FormatUint(v.Minor, 10))
	b.WriteByte('.')
	b.WriteString(strconv.FormatUint(v.Patch, 10))
	for i, p := range v.Pre {
		if i == 0 {
			b.WriteByte('-')
		} else {
			b.WriteByte('.')
		}
		b.WriteString(p.Str)
	}
	for i, id := range v.Build {
		if i == 0 {
			b.WriteByte('+')
		} else {
			b.WriteByte('.')
		}
		b.WriteString(id)
	}
	return b.String()
}

// Compare returns -1, 0 or 1 following SemVer 2.0.0 section 11 precedence. Build metadata is ignored.
func Compare(a, b Version) int {
	if c := cmp.Or(cmp.Compare(a.Major, b.Major), cmp.Compare(a.Minor, b.Minor), cmp.Compare(a.Patch, b.Patch)); c != 0 {
		return c
	}
	switch {
	case len(a.Pre) == 0 && len(b.Pre) == 0:
		return 0
	case len(a.Pre) == 0:
		return 1
	case len(b.Pre) == 0:
		return -1
	}
	for i := range min(len(a.Pre), len(b.Pre)) {
		if c := comparePreID(a.Pre[i], b.Pre[i]); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(a.Pre), len(b.Pre))
}

func comparePreID(a, b PreID) int {
	switch {
	case a.IsNum && b.IsNum:
		return cmp.Compare(a.Num, b.Num)
	case a.IsNum:
		return -1
	case b.IsNum:
		return 1
	}
	return strings.Compare(a.Str, b.Str)
}

// Equal reports whether the versions are identical, build metadata included.
func (v Version) Equal(o Version) bool {
	return v.Major == o.Major && v.Minor == o.Minor && v.Patch == o.Patch &&
		slices.Equal(v.Pre, o.Pre) && slices.Equal(v.Build, o.Build)
}

// IsPrerelease reports whether the version has a pre-release part.
func (v Version) IsPrerelease() bool { return len(v.Pre) > 0 }
