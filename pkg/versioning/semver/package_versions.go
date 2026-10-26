package semver

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/pkg/versioning/trace"
)

// ErrPreReleaseNotFound is returned by CalculateNewVersion when a Release rule is requested and the package has
// no pre-release to promote. Match it with errors.Is.
var ErrPreReleaseNotFound error = diag.New("No prerelease version found, but a Release rule was requested").
	WithCode("remeris::pre_release_not_found")

// PackageVersions is the version history of a package that matters for the next bump (spec F1.3): the latest
// stable version, if any, and the newest pre-release of each label for every core above it. So it can hold
// 1.2.3, 1.2.4-rc.1, 1.3.0-beta.0 and 2.0.0-alpha.4. The zero value is an empty history. Copies share their
// maps, so use Clone to get an independent history.
type PackageVersions struct {
	// stable has no pre-release. Build metadata is kept as it was read.
	stable *Version
	// prereleases maps a core to its newest pre-release for each PreLabel label. An inner map is never empty.
	prereleases map[Core]map[string]Version
}

// FromTags builds the history from Git tags, which must be in newest-first order (spec F1.3, F9.3). A tag is
// used when it starts with "v", or with "{prefix}/v" when prefix is not nil. The first occurrence of that pattern
// is removed and the rest is parsed as a Version; tags that do not parse are skipped. The first stable tag ends
// the scan, because only pre-releases newer than the latest stable version matter. The tracer t, which may be nil,
// gets Knope's "No tags found starting with {pattern}" line when no tag starts with the pattern.
func FromTags(prefix *string, tags []string, t *trace.Tracer) PackageVersions {
	pattern := "v"
	if prefix != nil {
		pattern = *prefix + "/v"
	}
	if !slices.ContainsFunc(tags, func(tag string) bool { return strings.HasPrefix(tag, pattern) }) {
		t.Debug("No tags found starting with " + pattern)
	}
	var p PackageVersions
	for _, tag := range tags {
		if !strings.HasPrefix(tag, pattern) {
			continue
		}
		// Like Knope's replacen(pattern, "", 1): only the first occurrence goes, which is the leading one.
		v, err := Parse(strings.Replace(tag, pattern, "", 1))
		if err != nil {
			continue
		}
		if !v.IsPrerelease() {
			p.stable = &v
			break
		}
		p.Update(v)
	}
	return p
}

// Clone returns a deep copy of the history.
func (p PackageVersions) Clone() PackageVersions {
	var c PackageVersions
	if p.stable != nil {
		v := cloneVersion(*p.stable)
		c.stable = &v
	}
	for core, labels := range p.prereleases {
		for label, v := range labels {
			c.setPre(core, label, cloneVersion(v))
		}
	}
	return c
}

func cloneVersion(v Version) Version {
	v.Pre, v.Build = slices.Clone(v.Pre), slices.Clone(v.Build)
	return v
}

// FromVersion returns the history that holds only v (spec F7.4): a stable v is the stable version, a
// pre-release is the only pre-release.
func FromVersion(v Version) PackageVersions {
	var p PackageVersions
	p.Update(v)
	return p
}

// Update records v when it is newer than what the history holds (spec F1.3). A stable v replaces the stable
// version, unless that one is greater or equal, and clears all pre-releases. A pre-release replaces the one with
// the same core and label, unless that one is greater or equal.
func (p *PackageVersions) Update(v Version) {
	if !v.IsPrerelease() {
		if p.stable != nil && Compare(*p.stable, v) >= 0 {
			return
		}
		v = cloneVersion(v)
		p.stable = &v
		p.prereleases = nil
		return
	}
	label, _, _ := v.PreLabel()
	core := v.Core()
	if old, ok := p.prereleases[core][label]; ok && Compare(old, v) >= 0 {
		return
	}
	p.setPre(core, label, cloneVersion(v))
}

func (p *PackageVersions) setPre(core Core, label string, v Version) {
	if p.prereleases == nil {
		p.prereleases = make(map[Core]map[string]Version)
	}
	if p.prereleases[core] == nil {
		p.prereleases[core] = make(map[string]Version)
	}
	p.prereleases[core][label] = v
}

// maxCore returns the largest core that has pre-releases.
func (p PackageVersions) maxCore() (Core, bool) {
	var best Core
	found := false
	for c := range p.prereleases {
		if !found || CompareCore(c, best) > 0 {
			best, found = c, true
		}
	}
	return best, found
}

// Latest returns the most recent version: the highest-precedence pre-release of the largest core, or else the
// stable version. Like Knope, pre-releases are not compared with the stable version: FromTags only keeps those
// newer than the latest stable tag, and a newer stable Update clears them. The bool is false for an empty history.
func (p PackageVersions) Latest() (Version, bool) {
	if c, ok := p.maxCore(); ok {
		var best Version
		first := true
		for _, v := range p.prereleases[c] {
			// Spec F1.3 (Q2): highest SemVer precedence. Knope took the largest label string.
			if first || Compare(v, best) > 0 {
				best, first = v, false
			}
		}
		return best, true
	}
	if p.stable != nil {
		return *p.stable, true
	}
	return Version{}, false
}

// LastStable returns the latest stable version, as read. The bool is false when there is none.
func (p PackageVersions) LastStable() (Version, bool) {
	if p.stable == nil {
		return Version{}, false
	}
	return *p.stable, true
}

// CalculateNewVersion applies the rule to the history (spec F1.3). A stable rule bumps the stable version; with no
// stable version it takes the largest pre-release core as it is, or 0.0.0. A pre rule gives the next
// "core-label.N". A release rule gives the largest pre-release core, or ErrPreReleaseNotFound. The tracer t, which
// may be nil, gets the debug lines Knope prints while it calculates the version.
func (p PackageVersions) CalculateNewVersion(r Rule, t *trace.Tracer) (Version, error) {
	switch r.Kind {
	case RulePre:
		return p.calculatePre(r.Label, r.Stable, t)
	case RuleRelease:
		c, ok := p.maxCore()
		if !ok {
			return Version{}, ErrPreReleaseNotFound
		}
		return c.Version(), nil
	default:
		return p.baseCore(r.Stable, t).Version(), nil
	}
}

// baseCore is the core a new version is built on: the stable version bumped by r or, with no stable version, the
// largest pre-release core as it is (assumed set by hand), or 0.0.0.
func (p PackageVersions) baseCore(r StableRule, t *trace.Tracer) Core {
	if p.stable != nil {
		return bumpStable(p.stable.Core(), r, t)
	}
	c, _ := p.maxCore()
	return c
}

func (p PackageVersions) calculatePre(label string, r StableRule, t *trace.Tracer) (Version, error) {
	t.Debug("Pre-release label " + label + " selected. Determining next stable version...")
	base := p.baseCore(r, t)
	var counter uint64
	// Spec F1.3 step 2: an existing "1.2.4-beta" without a counter is followed by "1.2.4-beta.0".
	if old, ok := p.prereleases[base][label]; ok {
		_, n, hasCounter := old.PreLabel()
		t.Debug("Found existing pre-release version " + label + "." + strconv.FormatUint(n, 10))
		if hasCounter {
			if n == math.MaxUint64 {
				return Version{}, diag.Newf("The pre-release counter of %s cannot be increased", old).
					WithCode("remeris::pre_release_counter_overflow")
			}
			counter = n + 1
		}
	}
	if counter == 0 {
		t.Debug("No existing pre-release version found; creating " + label + ".0")
	}
	return NewPre(base, label, counter)
}
