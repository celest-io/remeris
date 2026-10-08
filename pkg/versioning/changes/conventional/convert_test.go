package conventional_test

import (
	"reflect"
	"testing"

	"github.com/celest-io/remeris/pkg/versioning/changes"
	"github.com/celest-io/remeris/pkg/versioning/changes/conventional"
)

// footers is a changes.FooterLookup with ASCII case-insensitive matching, like releasenotes.Sections.
type footers []string

func (f footers) ContainsFooter(token string) bool { return changes.ContainsASCIIFold(f, token) }

const (
	bugFix      = "a bug"
	helloText   = "hello"
	noScope     = "No scope"
	fixNoScope  = "fix: No scope"
	breakingTok = "BREAKING CHANGE"
)

func commit(typ changes.Kind, summary, description string) changes.Change {
	return changes.Change{
		Type:    changes.ChangeType{Kind: typ},
		Summary: summary,
		Origin:  changes.Origin{Kind: changes.ConventionalCommit, Description: description},
	}
}

func msgs(messages ...string) []conventional.CommitMessage {
	out := make([]conventional.CommitMessage, len(messages))
	for i, m := range messages {
		out[i] = conventional.CommitMessage{Message: m}
	}
	return out
}

func TestChanges(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		commits  []conventional.CommitMessage
		scopes   []string
		sections changes.FooterLookup
		want     []changes.Change
	}{
		"commit types": {
			commits: msgs("fix: a bug", "fix!: a breaking bug fix", "feat!: add a feature", "feat: add another feature"),
			want: []changes.Change{
				commit(changes.Fix, bugFix, "fix: a bug"),
				commit(changes.Breaking, "a breaking bug fix", "fix!: a breaking bug fix"),
				commit(changes.Breaking, "add a feature", "feat!: add a feature"),
				commit(changes.Feature, "add another feature", "feat: add another feature"),
			},
		},
		"separate breaking messages": {
			commits: msgs(
				"fix: a bug\n\nBREAKING CHANGE: something broke",
				"feat: a features\n\nBREAKING CHANGE: something else broke",
			),
			want: []changes.Change{
				commit(changes.Breaking, "something broke", "fix: a bug\n\tContaining footer BREAKING CHANGE: something broke"),
				commit(changes.Fix, bugFix, "fix: a bug"),
				commit(changes.Breaking, "something else broke",
					"feat: a features\n\tContaining footer BREAKING CHANGE: something else broke"),
				commit(changes.Feature, "a features", "feat: a features"),
			},
		},
		"scopes used but none defined": {
			commits: msgs("feat(scope)!: Wrong scope breaking change!", fixNoScope),
			want: []changes.Change{
				commit(changes.Breaking, "Wrong scope breaking change!", "feat(scope)!: Wrong scope breaking change!"),
				commit(changes.Fix, noScope, fixNoScope),
			},
		},
		"filter scopes": {
			commits: msgs("feat(wrong_scope)!: Wrong scope breaking change!", "feat(scope): Scoped feature", fixNoScope),
			scopes:  []string{"scope"},
			want: []changes.Change{
				commit(changes.Feature, "Scoped feature", "feat(scope): Scoped feature"),
				commit(changes.Fix, noScope, fixNoScope),
			},
		},
		"empty scopes drop scoped commits": {
			commits: msgs("feat(scope): Scoped feature", fixNoScope),
			scopes:  []string{},
			want:    []changes.Change{commit(changes.Fix, noScope, fixNoScope)},
		},
		"scope is ASCII case-insensitive": {
			commits: msgs("feat(SCOPE): Scoped feature"),
			scopes:  []string{"scope"},
			want:    []changes.Change{commit(changes.Feature, "Scoped feature", "feat(SCOPE): Scoped feature")},
		},
		"scope is not Unicode case-insensitive": {
			commits: msgs("feat(K): Kelvin"),
			scopes:  []string{"k"},
			want:    nil,
		},
		"custom footers": {
			commits:  msgs("chore: ignored type\n\nignored-footer: ignored\ncustom-footer: hello"),
			sections: footers{"custom-footer"},
			want: []changes.Change{{
				Type:    changes.FooterType("custom-footer"),
				Summary: helloText,
				Origin: changes.Origin{
					Kind:        changes.ConventionalCommit,
					Description: "chore: ignored type\n\tContaining footer custom-footer: hello",
				},
			}},
		},
		"custom footer matches case-insensitively and keeps the written token": {
			commits:  msgs("chore: x\n\nCustom-Footer: hello"),
			sections: footers{"custom-footer"},
			want: []changes.Change{{
				Type:    changes.FooterType("Custom-Footer"),
				Summary: helloText,
				Origin: changes.Origin{
					Kind:        changes.ConventionalCommit,
					Description: "chore: x\n\tContaining footer Custom-Footer: hello",
				},
			}},
		},
		"footer with hash separator": {
			commits:  msgs("chore: x\n\nrefs #12"),
			sections: footers{"refs"},
			want: []changes.Change{{
				Type:    changes.FooterType("refs"),
				Summary: "12",
				Origin: changes.Origin{
					Kind:        changes.ConventionalCommit,
					Description: "chore: x\n\tContaining footer refs # 12",
				},
			}},
		},
		"footers come before the description change": {
			commits:  msgs("feat: a feature\n\nnote: hello\nBREAKING CHANGE: broke"),
			sections: footers{"note"},
			want: []changes.Change{
				{
					Type:    changes.FooterType("note"),
					Summary: helloText,
					Origin: changes.Origin{
						Kind:        changes.ConventionalCommit,
						Description: "feat: a feature\n\tContaining footer note: hello",
					},
				},
				commit(changes.Breaking, "broke", "feat: a feature\n\tContaining footer BREAKING CHANGE: broke"),
				commit(changes.Feature, "a feature", "feat: a feature"),
			},
		},
		"lowercase breaking footer": {
			commits: msgs("fix: a bug\n\nbreaking-change: broke"),
			want: []changes.Change{
				commit(changes.Breaking, "broke", "fix: a bug\n\tContaining footer breaking-change: broke"),
				commit(changes.Fix, bugFix, "fix: a bug"),
			},
		},
		"breaking footer and exclamation mark": {
			commits: msgs("feat!: a feature\n\n" + breakingTok + ": broke"),
			want: []changes.Change{
				commit(changes.Breaking, "broke", "feat: a feature\n\tContaining footer BREAKING CHANGE: broke"),
				commit(changes.Feature, "a feature", "feat: a feature"),
			},
		},
		"breaking footer on a chore has no description change": {
			commits: msgs("chore!: tidy\n\n" + breakingTok + ": broke"),
			want: []changes.Change{
				commit(changes.Breaking, "broke", "chore: tidy\n\tContaining footer BREAKING CHANGE: broke"),
			},
		},
		"exclamation mark on a chore is breaking": {
			commits: msgs("chore!: tidy"),
			want:    []changes.Change{commit(changes.Breaking, "tidy", "chore!: tidy")},
		},
		"type is case-insensitive": {
			commits: msgs("FEAT: shout", "Fix: mixed"),
			want: []changes.Change{
				commit(changes.Feature, "shout", "FEAT: shout"),
				commit(changes.Fix, "mixed", "Fix: mixed"),
			},
		},
		"unparseable commits are skipped": {
			commits:  msgs("just a message", "Merge branch 'x'\n\nfoo: bar", "fix: fine"),
			sections: footers{"foo"},
			want:     []changes.Change{commit(changes.Fix, "fine", "fix: fine")},
		},
		"other types are not changes": {
			commits: msgs("docs: words", "chore: tidy"),
			want:    nil,
		},
		"messages are trimmed": {
			commits: msgs("\n  \nfix: padded\n\n"),
			want:    []changes.Change{commit(changes.Fix, "padded", "fix: padded")},
		},
		"nil sections list no footers": {
			commits:  msgs("fix: a bug\n\nnote: hello"),
			sections: nil,
			want:     []changes.Change{commit(changes.Fix, bugFix, "fix: a bug")},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := conventional.Changes(tt.commits, tt.scopes, tt.sections)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got  %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestChangesGitInfo(t *testing.T) {
	t.Parallel()
	info := &changes.GitInfo{Hash: "abcdef1", AuthorName: "Bob"}
	got := conventional.Changes([]conventional.CommitMessage{
		{Message: "fix: a bug\n\nBREAKING CHANGE: broke", Info: info},
		{Message: "feat: no info"},
	}, nil, nil)
	if len(got) != 3 {
		t.Fatalf("got %d changes, want 3", len(got))
	}
	if got[0].GitInfo != info || got[1].GitInfo != info {
		t.Error("every change of a commit must carry its GitInfo")
	}
	if got[2].GitInfo != nil {
		t.Error("a commit without info gives changes without GitInfo")
	}
}
