package semver

import (
	"strings"

	"github.com/celest-io/remeris/internal/diag"
)

// ErrPreReleaseNotFound is returned by CalculateNewVersion when a Release rule is requested and the package has
// no pre-release to promote. Match it with errors.Is.
var ErrPreReleaseNotFound error = diag.New("No prerelease version found, but a Release rule was requested").
	WithCode("remeris::pre_release_not_found")

// PackageVersions is the version history of a package that matters for the next bump (spec F1.3): the latest
// stable version, if any, and the newest pre-release of each label for every core above it. So it can hold
// 1.2.3, 1.2.4-rc.1, 1.3.0-beta.0 and 2.0.0-alpha.4. The zero value is an empty history.
type PackageVersions struct {
	// stable has no pre-release. Build metadata is kept as it was read.
	stable *Version
	// prereleases maps a core to its newest pre-release for each PreLabel label. An inner map is never empty.
	prereleases map[Core]map[string]Version
}

// FromTags builds the history from Git tags, which must be in newest-first order (spec F1.3, F9.3). A tag is
// used when it starts with "v", or with "{prefix}/v" when prefix is not nil. The first occurrence of that pattern
// is removed and the rest is parsed as a Version; tags that do not parse are skipped. The first stable tag ends
// the scan, because only pre-releases newer than the latest stable version matter.
func FromTags(prefix *string, tags []string) PackageVersions {
	pattern := "v"
	if prefix != nil {
		pattern = *prefix + "/v"
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
		p.stable = &v
		p.prereleases = nil
		return
	}
	label, _, _ := v.PreLabel()
	core := v.Core()
	if old, ok := p.prereleases[core][label]; ok && Compare(old, v) >= 0 {
		return
	}
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

// Latest returns the most recent version: the highest-precedence pre-release of the largest core above the
// stable version, or else the stable version. The bool is false for an empty history.
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
// "core-label.N". A release rule gives the largest pre-release core, or ErrPreReleaseNotFound.
func (p PackageVersions) CalculateNewVersion(r Rule) (Version, error) {
	switch r.Kind {
	case RulePre:
		return p.calculatePre(r.Label, r.Stable)
	case RuleRelease:
		c, ok := p.maxCore()
		if !ok {
			return Version{}, ErrPreReleaseNotFound
		}
		return c.Version(), nil
	default:
		return p.baseCore(r.Stable).Version(), nil
	}
}

// baseCore is the core a new version is built on: the stable version bumped by r or, with no stable version, the
// largest pre-release core as it is (assumed set by hand), or 0.0.0.
func (p PackageVersions) baseCore(r StableRule) Core {
	if p.stable != nil {
		return BumpStable(p.stable.Core(), r)
	}
	c, _ := p.maxCore()
	return c
}

func (p PackageVersions) calculatePre(label string, r StableRule) (Version, error) {
	base := p.baseCore(r)
	var counter uint64
	// Spec F1.3 step 2: an existing "1.2.4-beta" without a counter is followed by "1.2.4-beta.0".
	if old, ok := p.prereleases[base][label]; ok {
		if _, n, hasCounter := old.PreLabel(); hasCounter {
			counter = n + 1
		}
	}
	return NewPre(base, label, counter)
}
