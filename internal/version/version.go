// Package version holds the build information. The variables are set at link time by the build
// task (mise-tasks/build) with -ldflags -X. A binary built without them (go build, go install module@version)
// gets what it can from the build information the Go toolchain embeds; anything still unknown stays "unknown".
package version

import (
	"fmt"
	"runtime/debug"
	"time"
)

const (
	unknown      = "unknown"
	develVersion = "(devel)"
	vcsRevision  = "vcs.revision"
	vcsModified  = "vcs.modified"
	vcsTime      = "vcs.time"
	buildDateFmt = "2006-01-02_15:04:05"
)

var (
	Version     = unknown
	Revision    = unknown
	Branch      = unknown
	BuildUser   = unknown
	BuildDate   = unknown
	BuildNumber = unknown
	GoVersion   = unknown
)

// Build holds the variables that the build information can fill when the -ldflags did not set them.
type Build struct {
	Version   string
	Revision  string
	BuildDate string
	GoVersion string
}

// withBuildInfo returns b with each unknown field filled from bi. A value that is not unknown (set by -ldflags)
// always wins. A nil bi, which ReadBuildInfo returns for a binary without build information, changes nothing.
func withBuildInfo(bi *debug.BuildInfo, b Build) Build {
	if bi == nil {
		return b
	}
	if b.Version == unknown && bi.Main.Version != "" && bi.Main.Version != develVersion {
		b.Version = bi.Main.Version
	}
	if b.GoVersion == unknown && bi.GoVersion != "" {
		b.GoVersion = bi.GoVersion
	}

	settings := make(map[string]string, len(bi.Settings))
	for _, s := range bi.Settings {
		settings[s.Key] = s.Value
	}
	if b.Revision == unknown && settings[vcsRevision] != "" {
		b.Revision = settings[vcsRevision]
		if settings[vcsModified] == "true" {
			b.Revision += "-dirty" // like git describe --dirty: the binary was built from a modified tree
		}
	}
	if b.BuildDate == unknown && settings[vcsTime] != "" {
		b.BuildDate = formatBuildDate(settings[vcsTime])
	}
	return b
}

// formatBuildDate writes the commit time in the format of the build task (UTC). A time that does not parse
// is kept as it is rather than dropped.
func formatBuildDate(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.UTC().Format(buildDateFmt)
}

// init fills the variables that the -ldflags left unknown from the build information of this binary.
func init() {
	bi, _ := debug.ReadBuildInfo() // nil when the binary has no build information
	b := withBuildInfo(bi, Build{Version: Version, Revision: Revision, BuildDate: BuildDate, GoVersion: GoVersion})
	Version, Revision, BuildDate, GoVersion = b.Version, b.Revision, b.BuildDate, b.GoVersion
}

// Full returns the version with its build details, for --version.
func Full() string {
	return fmt.Sprintf(
		"%s [%s/%s.%s] (Go: %s, User: %s, Date: %s)",
		Version,
		Branch,
		Revision,
		BuildNumber,
		GoVersion,
		BuildUser,
		BuildDate,
	)
}

// ServiceVersion returns a short identifier of the build, for telemetry.
func ServiceVersion() string {
	return fmt.Sprintf("%s/%s.%s", Branch, Revision, BuildNumber)
}
