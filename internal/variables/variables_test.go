package variables

import (
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/workflow"
)

const (
	echoHi        = "echo hi"
	msgNoPackages = "No packages to operate on"
	msgNoIssue    = "No issue selected"
	keyV          = "$v"
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
		"version without packages": {Version, msgNoPackages, "There must be at least one package for remeris"},
		"changelog without packages": {
			ChangelogEntry,
			msgNoPackages,
			"There must be at least one package for remeris",
		},
		"issue branch": {
			IssueBranch,
			msgNoIssue,
			"The IssueBranch command variable requires selecting an issue first with SelectGitHubIssue or SelectJiraIssue",
		},
		"unknown": {"Nope", `Unknown variable "Nope"`, ""},
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

func TestResolveIssueBranch(t *testing.T) {
	cases := map[string]struct {
		issue workflow.Issue
		want  string
	}{
		"github":       {workflow.Issue{Key: "123", Summary: "Test issue"}, "123-test-issue"},
		"jira":         {workflow.Issue{Key: "FLOW-5", Summary: "A test issue"}, "FLOW-5-a-test-issue"},
		"non ascii":    {workflow.Issue{Key: "1", Summary: "ÉCOLE Ab/c"}, "1-École-ab/c"},
		"other spaces": {workflow.Issue{Key: "2", Summary: "a\tB  c"}, "2-a\tb--c"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := Resolve(workflow.State{Issue: &tc.issue}, IssueBranch, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("IssueBranch = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveIssueBranchCode(t *testing.T) {
	_, err := Resolve(workflow.State{}, IssueBranch, nil)
	d, ok := err.(diag.Diagnostic)
	if !ok || d.Code() != "remeris::variables::no_issue_selected" {
		t.Errorf("err = %#v", err)
	}
}

func TestTemplateReplace(t *testing.T) {
	cases := map[string]struct {
		tmpl    Template
		want    string
		wantErr string
	}{
		"no variables in text": {
			tmpl: Template{Text: echoHi, Variables: []Entry{{keyV, Version}}},
			want: echoHi,
		},
		"unused keys are skipped, even unresolvable ones": {
			tmpl: Template{Text: echoHi, Variables: []Entry{{keyV, Version}, {"$i", IssueBranch}, {"$c", ChangelogEntry}}},
			want: echoHi,
		},
		"used key that cannot resolve fails": {
			tmpl:    Template{Text: "echo $v", Variables: []Entry{{keyV, Version}}},
			wantErr: msgNoPackages,
		},
		"absent variables apply the defaults": {
			tmpl:    Template{Text: "echo $version"},
			wantErr: msgNoPackages,
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
			wantErr: msgNoIssue,
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
	if _, err := first.Replace(workflow.State{}); err == nil || err.Error() != msgNoPackages {
		t.Errorf("declared order a,b: got %v", err)
	}
	if _, err := second.Replace(workflow.State{}); err == nil || err.Error() != msgNoIssue {
		t.Errorf("declared order b,a: got %v", err)
	}
}
