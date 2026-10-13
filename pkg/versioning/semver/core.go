package semver

import (
	"cmp"
	"strconv"
)

// Core is the stable component of a version: major.minor.patch without pre-release or build metadata. Knope calls
// it the "stable component". It is comparable, so it can key a map.
type Core struct{ Major, Minor, Patch uint64 }

// Core returns the stable component of v.
func (v Version) Core() Core { return Core{Major: v.Major, Minor: v.Minor, Patch: v.Patch} }

// Version returns the stable version with this core, with no pre-release and no build metadata.
func (c Core) Version() Version { return Version{Major: c.Major, Minor: c.Minor, Patch: c.Patch} }

// String returns "major.minor.patch".
func (c Core) String() string {
	return strconv.FormatUint(c.Major, 10) + "." + strconv.FormatUint(c.Minor, 10) + "." + strconv.FormatUint(c.Patch, 10)
}

// CompareCore returns -1, 0 or 1 comparing major, then minor, then patch.
func CompareCore(a, b Core) int {
	return cmp.Or(cmp.Compare(a.Major, b.Major), cmp.Compare(a.Minor, b.Minor), cmp.Compare(a.Patch, b.Patch))
}
