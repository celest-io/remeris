package steps_test

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/prompt"
	"github.com/celest-io/remeris/internal/steps"
	"github.com/celest-io/remeris/internal/workflow"
	"github.com/celest-io/remeris/pkg/versioning"
	"github.com/celest-io/remeris/pkg/versioning/releasenotes"
)

const (
	changeFileName = ".changeset/add_a_thing.md"
	changeSummary  = "Add a thing!"
	typeQuestion   = "What type of change is this?"
	summaryAsk     = "What is a short summary of this change?"
	summaryHelp    = "This will be used as a header in the changelog"
	defaultMajor   = "---\ndefault: major\n---\n\n# Add a thing!\n"
)

func changePackages(t *testing.T, sections releasenotes.Sections, names ...string) []*versioning.Package {
	t.Helper()
	var out []*versioning.Package
	for _, name := range names {
		pn := versioning.DefaultName()
		if name != "" {
			pn = versioning.CustomName(name)
		}
		pkg, err := versioning.NewPackage(pn, nil, nil, nil, 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		pkg.ReleaseNotes.Sections = sections
		out = append(out, pkg)
	}
	return out
}

func sameQuestion(a, b prompt.Asked) bool {
	return a.Kind == b.Kind && a.Message == b.Message && a.Help == b.Help && a.AtLeastOne == b.AtLeastOne &&
		slices.Equal(a.Options, b.Options)
}

func TestCreateChangeFile(t *testing.T) {
	custom := releasenotes.ConvertSections([]releasenotes.ChangelogSectionConfig{
		{Name: "Docs", Types: []string{"docs"}, Footers: []string{"Doc"}},
	})
	builtins := strings.Fields("major minor patch")
	withDocs := append(slices.Clone(builtins), "docs")
	summary := prompt.Asked{Kind: prompt.KindInput, Message: summaryAsk, Help: summaryHelp}
	cases := map[string]struct {
		names    []string
		sections releasenotes.Sections
		answers  []prompt.Answer
		existing string
		want     string
		asked    []prompt.Asked
	}{
		"default package": {
			names:   []string{""},
			answers: []prompt.Answer{{Index: 1}, {Text: changeSummary}},
			want:    "---\ndefault: minor\n---\n\n# Add a thing!\n",
			asked:   []prompt.Asked{{Kind: prompt.KindSelect, Message: typeQuestion, Options: builtins}, summary},
		},
		"custom package": {
			names:   []string{"foo"},
			answers: []prompt.Answer{{Index: 2}, {Text: changeSummary}},
			want:    "---\nfoo: patch\n---\n\n# Add a thing!\n",
			asked: []prompt.Asked{
				{Kind: prompt.KindSelect, Message: "What type of change is this for foo?", Options: builtins}, summary,
			},
		},
		"selection order and custom types": {
			names:    []string{"a", "b", "c"},
			sections: custom,
			answers:  []prompt.Answer{{Indexes: []int{2, 0}}, {Index: 3}, {Index: 0}, {Text: changeSummary}},
			want:     "---\nc: docs\na: major\n---\n\n# Add a thing!\n",
			asked: []prompt.Asked{
				{
					Kind: prompt.KindMultiSelect, Message: "Which packages does this change affect?",
					Options: []string{"a", "b", "c"}, AtLeastOne: "At least one package must be selected",
				},
				{Kind: prompt.KindSelect, Message: "What type of change is this for c?", Options: withDocs},
				{Kind: prompt.KindSelect, Message: "What type of change is this for a?", Options: withDocs},
				summary,
			},
		},
		"overwrites an existing file": {
			names:    []string{""},
			answers:  []prompt.Answer{{Index: 0}, {Text: changeSummary}},
			existing: "old",
			want:     defaultMajor,
		},
		"normalizes the summary into the file name": {
			names:   []string{""},
			answers: []prompt.Answer{{Index: 0}, {Text: "Add  a_thing!"}},
			want:    "---\ndefault: major\n---\n\n# Add  a_thing!\n",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if tc.existing != "" {
				if err := os.MkdirAll(".changeset", 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(changeFileName, []byte(tc.existing), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			p := prompt.NewScripted(tc.answers...)
			state := workflow.State{Packages: changePackages(t, tc.sections, tc.names...), Prompter: p}
			if _, err := (&steps.CreateChangeFile{}).Run(context.Background(), workflow.Real(state)); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(changeFileName)
			if err != nil || string(got) != tc.want {
				t.Errorf("file = %q (%v), want %q", got, err, tc.want)
			}
			if tc.asked != nil && !slices.EqualFunc(p.Asked(), tc.asked, sameQuestion) {
				t.Errorf("asked = %+v, want %+v", p.Asked(), tc.asked)
			}
		})
	}
}

func TestCreateChangeFileDryRun(t *testing.T) {
	t.Chdir(t.TempDir())
	p := prompt.NewScripted()
	state := workflow.State{Packages: changePackages(t, nil, ""), Prompter: p}
	if _, err := (&steps.CreateChangeFile{}).Run(context.Background(), workflow.DryRun(state)); err != nil {
		t.Fatal(err)
	}
	if len(p.Asked()) != 0 {
		t.Errorf("asked %v in a dry run", p.Asked())
	}
	if _, err := os.Stat(".changeset"); err == nil {
		t.Error(".changeset was created in a dry run")
	}
}

func TestCreateChangeFileErrors(t *testing.T) {
	errInterrupted := errors.New("interrupted")
	fail := prompt.Answer{Err: prompt.Error(errInterrupted)}
	cases := map[string]struct {
		names      []string
		answers    []prompt.Answer
		noPrompter bool
		want       error
	}{
		"select error":   {names: []string{""}, answers: []prompt.Answer{fail}, want: errInterrupted},
		"input error":    {names: []string{""}, answers: []prompt.Answer{{Index: 0}, fail}, want: errInterrupted},
		"multi error":    {names: []string{"a", "b"}, answers: []prompt.Answer{fail}, want: errInterrupted},
		"no answer left": {names: []string{""}, want: prompt.ErrScript},
		"no packages":    {},
		"no prompter":    {names: []string{""}, noPrompter: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			state := workflow.State{Packages: changePackages(t, nil, tc.names...)}
			if !tc.noPrompter {
				state.Prompter = prompt.NewScripted(tc.answers...)
			}
			_, err := (&steps.CreateChangeFile{}).Run(context.Background(), workflow.Real(state))
			if err == nil {
				t.Fatal("expected an error")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
			if _, statErr := os.Stat(".changeset"); statErr == nil {
				t.Error("nothing should be written")
			}
		})
	}
}
