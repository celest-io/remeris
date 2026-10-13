package semver

import (
	"strconv"
	"strings"
)

// PreLabel returns Knope's (label, counter) view of the pre-release. With at least two identifiers and a numeric
// last one, the counter is that last identifier and the label is the rest joined with ".". Otherwise the label
// is the whole pre-release and hasCounter is false. A single numeric identifier is the exception: the label is
// empty and the counter is that number. A stable version returns zero values.
func (v Version) PreLabel() (label string, counter uint64, hasCounter bool) {
	n := len(v.Pre)
	if n == 0 {
		return "", 0, false
	}
	last := v.Pre[n-1]
	if !last.IsNum {
		return joinPre(v.Pre), 0, false
	}
	return joinPre(v.Pre[:n-1]), last.Num, true
}

func joinPre(ids []PreID) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = id.Str
	}
	return strings.Join(parts, ".")
}

// NewPre returns the pre-release "core-label.counter". The label may contain dots. An empty label gives
// "core-counter", the inverse of PreLabel for a single numeric identifier. Knope accepts any label and fails
// later when it reads the version back; NewPre instead returns a *ParseError when the label and counter do not
// form a valid SemVer pre-release.
func NewPre(c Core, label string, counter uint64) (Version, error) {
	v := c.Version()
	if label != "" {
		for id := range strings.SplitSeq(label, ".") {
			p, detail := parsePreID(id)
			if detail != "" {
				return Version{}, newParseError(detail)
			}
			v.Pre = append(v.Pre, p)
		}
	}
	v.Pre = append(v.Pre, PreID{Str: strconv.FormatUint(counter, 10), Num: counter, IsNum: true})
	return v, nil
}
