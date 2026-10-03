// Package brand holds the names remeris uses for itself, in one place (spec D1).
//
// The configuration file keeps Knope's name so existing Knope projects work unchanged.
// Everything else says "remeris".
package brand

const (
	// Name is the binary name, used in help, log and error text.
	Name = "remeris"

	// ConfigFile is the project configuration file, shared with Knope.
	ConfigFile = "knope.toml"

	// PrereleaseLabelEnv sets the pre-release label of PrepareRelease steps.
	PrereleaseLabelEnv = "REMERIS_PRERELEASE_LABEL"

	// LegacyPrereleaseLabelEnv is Knope's variable, read as a fallback to ease migrating CI.
	LegacyPrereleaseLabelEnv = "KNOPE_PRERELEASE_LABEL"
)
