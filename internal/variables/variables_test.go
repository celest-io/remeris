package variables

import (
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/workflow"
)

func TestVariableValid(t *testing.T) {
	for _, v := range []Variable{Version, ChangelogEntry, IssueBranch} {
		if !v.Valid() {
			t.Errorf("%q should be valid", v)
		}
	}
	for _, v := range []Variable{"", "version", "Other"} {
		if v.Valid() {
			t.Errorf("%q should be invalid", v)
		}
	}
}

func TestResolve(t *testing.T) {
	cases := map[string]struct {
		variable Variable
		wantMsg  string
		wantHelp string
	}{
		"version without packages":   {Version, "No packages to operate on", "There must be at least one package for remeris"},
		"changelog without packages": {ChangelogEntry, "No packages to operate on", "There must be at least one package for remeris"},
		"issue branch":               {IssueBranch, "No issue selected", "select an issue"},
		"unknown":                    {"Nope", `Unknown variable "Nope"`, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Resolve(workflow.State{}, tc.variable, nil)
			if err == nil {
				t.Fatal("expected an error")
			}
			if err.Error() != tc.wantMsg {
				t.Errorf("message = %q, want %q", err.Error(), tc.wantMsg)
			}
			if d, ok := err.(diag.Diagnostic); !ok || !strings.Contains(d.Help(), tc.wantHelp) {
				t.Errorf("help = %v, want it to contain %q", err, tc.wantHelp)
			}
		})
	}
}

func TestTemplateReplace(t *testing.T) {
	cases := map[string]struct {
		tmpl    Template
		want    string
		wantErr string
	}{
		"no variables in text": {
			tmpl: Template{Text: "echo hi", Variables: []Entry{{"$v", Version}}},
			want: "echo hi",
		},
		"unused keys are skipped, even unresolvable ones": {
			tmpl: Template{Text: "echo hi", Variables: []Entry{{"$v", Version}, {"$i", IssueBranch}, {"$c", ChangelogEntry}}},
			want: "echo hi",
		},
		"used key that cannot resolve fails": {
			tmpl:    Template{Text: "echo $v", Variables: []Entry{{"$v", Version}}},
			wantErr: "No packages to operate on",
		},
		"absent variables apply the defaults": {
			tmpl:    Template{Text: "echo $version"},
			wantErr: "No packages to operate on",
		},
		"defaults skip text without their keys": {
			tmpl: Template{Text: "echo $other"},
			want: "echo $other",
		},
		"empty variables turn the defaults off": {
			tmpl: Template{Text: "echo $version $changelog", Variables: []Entry{}},
			want: "echo $version $changelog",
		},
		"a declared key replaces the default for its name": {
			tmpl:    Template{Text: "echo $version", Variables: []Entry{{"$version", IssueBranch}}},
			wantErr: "No issue selected",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := tc.tmpl.Replace(workflow.State{})
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("Replace() = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

// Resolve always fails in M0, so replacement order is checked through which error comes first:
// entries are applied in declared order, and only those whose key is in the text.
func TestTemplateReplaceOrder(t *testing.T) {
	text := "$a $b"
	first := Template{Text: text, Variables: []Entry{{"$a", Version}, {"$b", IssueBranch}}}
	second := Template{Text: text, Variables: []Entry{{"$b", IssueBranch}, {"$a", Version}}}
	if _, err := first.Replace(workflow.State{}); err == nil || err.Error() != "No packages to operate on" {
		t.Errorf("declared order a,b: got %v", err)
	}
	if _, err := second.Replace(workflow.State{}); err == nil || err.Error() != "No issue selected" {
		t.Errorf("declared order b,a: got %v", err)
	}
}
