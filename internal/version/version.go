// Package version holds the build information. The variables are set at link time by the build
// task (mise-tasks/build) with -ldflags -X; a plain `go build` leaves them as "unknown".
package version

import "fmt"

var (
	Version     = "unknown"
	Revision    = "unknown"
	Branch      = "unknown"
	BuildUser   = "unknown"
	BuildDate   = "unknown"
	BuildNumber = "unknown"
	GoVersion   = "unknown"
)

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
