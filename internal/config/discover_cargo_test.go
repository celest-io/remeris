package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
)

func TestCargoWorkspaceRoot(t *testing.T) {
	cases := map[string]struct {
		files    map[string]string
		wantCode string
	}{
		"missing Cargo.toml":        {files: map[string]string{goModName: ""}},
		"empty Cargo.toml":          {files: map[string]string{cargoTomlName: ""}},
		"package without workspace": {files: map[string]string{cargoTomlName: packageHeader}},
		"workspace without members": {files: map[string]string{cargoTomlName: "[workspace]\n"}},
		"empty members":             {files: map[string]string{cargoTomlName: "[workspace]\nmembers = []\n"}},
		"does not parse":            {files: map[string]string{cargoTomlName: "[[["}, wantCode: codeCargoTOML},
		"unterminated string":       {files: map[string]string{cargoTomlName: "a = \"x"}, wantCode: codeCargoTOML},
		"workspace of members": {
			files:    map[string]string{cargoTomlName: cargoMembers},
			wantCode: codeDiscoveryNotImplemented,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			pkgs, err := cargoWorkspace(rootOf(tc.files))
			if pkgs != nil {
				t.Errorf("packages = %+v, want none", pkgs)
			}
			if tc.wantCode == "" {
				if err != nil {
					t.Fatalf("error = %v, want none", err)
				}
				return
			}
			var d *diag.Error
			if !errors.As(err, &d) || d.Code() != tc.wantCode {
				t.Fatalf("error = %v, want code %s", err, tc.wantCode)
			}
		})
	}
}

func TestCargoParseErrorStopsDiscovery(t *testing.T) {
	_, err := discoverPackages(rootOf(map[string]string{cargoTomlName: "[[[", goModName: ""}))
	var d *diag.Error
	if !errors.As(err, &d) || d.Code() != codeCargoTOML || errors.Unwrap(d) == nil {
		t.Fatalf("error = %v, want the TOML error with its cause", err)
	}
	if !strings.HasPrefix(d.Error(), "Could not parse TOML in Cargo.toml: ") {
		t.Errorf("message = %q", d.Error())
	}
}
