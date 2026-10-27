package cli

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/semver"
	"github.com/celest-io/remeris/pkg/versioning/versionedfile"
)

// A warning of a versioned file goes through the logger that setupLogging installs.
func TestSetupLoggingInstallsWarnSink(t *testing.T) {
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old); versionedfile.SetWarnSink(nil) })
	var buf bytes.Buffer
	setupLogging(&buf, false, false)

	cfg := versionedfile.Config{Path: "package-lock.json", Format: versionedfile.PackageLockJson}
	f, err := versionedfile.New(cfg, `{"version":"1.0.0","lockfileVersion":1}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.SetVersion(semver.MustParse("1.1.0"), nil, versionedfile.Standard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "lockfileVersion is not 2 or 3") {
		t.Errorf("output = %q, want the lock file warning", buf.String())
	}
}
