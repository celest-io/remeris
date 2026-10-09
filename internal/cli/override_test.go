package cli

import (
	"strings"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning"
)

const (
	errFormat = "package override should be formatted like package_name=version"
	ver1      = "1.0.0"
	aEq1      = "a=1.0.0"
)

func pkgs(names ...string) []*versioning.Package {
	var out []*versioning.Package
	for _, n := range names {
		pn := versioning.DefaultName()
		if n != "" {
			pn = versioning.CustomName(n)
		}
		out = append(out, &versioning.Package{Name: pn})
	}
	return out
}

func overrides(ps []*versioning.Package) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		if p.Override != nil {
			out[i] = p.Override.String()
		}
	}
	return out
}

func TestApplyOverrides(t *testing.T) {
	cases := map[string]struct {
		names   []string
		values  []string
		want    []string
		wantErr string
	}{
		"no values":             {[]string{"a", "b"}, nil, []string{"", ""}, ""},
		"no packages":           {nil, []string{ver1}, []string{}, ""},
		"single package":        {[]string{""}, []string{"1.2.3-rc.1+b"}, []string{"1.2.3-rc.1+b"}, ""},
		"single package, twice": {[]string{""}, []string{ver1, "2.0.0"}, nil, "cannot be used multiple times"},
		"single package, bad":   {[]string{""}, []string{"1.0"}, nil, "Version must have exactly 3 parts"},
		"single package, v":     {[]string{""}, []string{"v1.0.0"}, nil, "invalid digit found in string"},
		"multiple":              {[]string{"a", "b", "c"}, []string{"c=3.0.0", aEq1}, []string{ver1, "", "3.0.0"}, ""},
		"multiple, no format":   {[]string{"a", "b"}, []string{ver1}, nil, errFormat},
		"multiple, bad version": {[]string{"a", "b"}, []string{"a=x"}, nil, "invalid digit found in string"},
		"unknown": {
			[]string{"a", "b"},
			[]string{"z=1.0.0", aEq1, "c=1.0.0"},
			nil,
			"Unknown package(s) to override: z, c",
		},
		"duplicate is unknown": {
			[]string{"a", "b"},
			[]string{aEq1, "a=2.0.0"},
			nil,
			"Unknown package(s) to override: a",
		},
		"default is not a name": {
			[]string{"", "b"},
			[]string{"default=1.0.0"},
			nil,
			"Unknown package(s) to override: default",
		},
		"build metadata kept": {[]string{"a", "b"}, []string{"b=1.0.0+x.y"}, []string{"", "1.0.0+x.y"}, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ps := pkgs(tc.names...)

			err := applyOverrides(ps, tc.values)

			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := overrides(ps)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("package %d override = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}
