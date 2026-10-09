package variables

import (
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/workflow"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/semver"
)

func pkgWith(tags ...string) *versioning.Package {
	return &versioning.Package{Versions: semver.FromTags(nil, tags)}
}

func TestResolveVersion(t *testing.T) {
	cases := map[string]struct {
		packages []*versioning.Package
		want     string
		wantMsg  string
		wantHelp string
	}{
		"no packages": {nil, "", msgNoPackages, "at least one package"},
		"one package": {[]*versioning.Package{pkgWith("v1.2.3", "v1.0.0")}, "1.2.3", "", ""},
		"prerelease":  {[]*versioning.Package{pkgWith("v1.3.0-rc.1")}, "1.3.0-rc.1", "", ""},
		"two packages": {
			[]*versioning.Package{pkgWith("v1.0.0"), pkgWith("v2.0.0")},
			"",
			"Too many packages defined",
			"single [package]",
		},
		"no version found": {[]*versioning.Package{pkgWith()}, "", "Could not determine version of package", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := Resolve(workflow.State{Packages: tc.packages}, Version, nil)

			if tc.wantMsg != "" {
				if err == nil || err.Error() != tc.wantMsg {
					t.Fatalf("err = %v, want %q", err, tc.wantMsg)
				}
				if d, ok := err.(diag.Diagnostic); tc.wantHelp != "" && (!ok || !strings.Contains(d.Help(), tc.wantHelp)) {
					t.Errorf("help = %v, want it to contain %q", err, tc.wantHelp)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("Resolve() = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestTemplateReplaceVersion(t *testing.T) {
	state := workflow.State{Packages: []*versioning.Package{pkgWith("v1.2.3")}}

	got, err := Template{Text: "v is $version, $version again"}.Replace(state)

	if err != nil || got != "v is 1.2.3, 1.2.3 again" {
		t.Errorf("Replace() = %q, %v", got, err)
	}
}

func TestResolveChangelogEntryChecksPackagesFirst(t *testing.T) {
	_, err := Resolve(workflow.State{Packages: []*versioning.Package{pkgWith(), pkgWith()}}, ChangelogEntry, nil)
	if err == nil || err.Error() != "Too many packages defined" {
		t.Errorf("err = %v", err)
	}
	_, err = Resolve(workflow.State{Packages: []*versioning.Package{pkgWith("v1.0.0")}}, ChangelogEntry, nil)
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("err = %v", err)
	}
}
