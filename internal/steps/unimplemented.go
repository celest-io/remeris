package steps

import (
	"context"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/variables"
	"github.com/celest-io/remeris/internal/workflow"
)

// The steps below are decoded from knope.toml but not implemented yet. Each one fails, in a real
// run and in a dry run alike, so `--validate` reports it. When a step gets its implementation it
// moves to a file of its own and the milestone constant disappears.

// BumpVersionRule is the `rule` of a BumpVersion step.
type BumpVersionRule string

const (
	RuleMajor   BumpVersionRule = "Major"
	RuleMinor   BumpVersionRule = "Minor"
	RulePatch   BumpVersionRule = "Patch"
	RulePre     BumpVersionRule = "Pre"
	RuleRelease BumpVersionRule = "Release"
)

// BumpVersion bumps the version of the packages (milestone M3).
type BumpVersion struct {
	Rule BumpVersionRule `toml:"rule"`
	// Label is the pre-release label. It is set if and only if Rule is RulePre.
	Label string `toml:"label"`
}

// PrepareRelease updates versions and changelogs from the pending changes (milestone M5).
type PrepareRelease struct {
	PrereleaseLabel           string `toml:"prerelease_label"`
	AllowEmpty                bool   `toml:"allow_empty"`
	IgnoreConventionalCommits bool   `toml:"ignore_conventional_commits"`
}

// Release tags and publishes the prepared release (milestone M6).
type Release struct{}

// CreateChangeFile writes a new change file (milestone M4).
type CreateChangeFile struct{}

// CreatePullRequest opens a pull request on the forge (milestone M7).
type CreatePullRequest struct {
	Base  string             `toml:"base"`
	Title variables.Template `toml:"title"`
	Body  variables.Template `toml:"body"`
}

// SelectJiraIssue selects a Jira issue (milestone M11).
type SelectJiraIssue struct {
	Status string `toml:"status"`
}

// TransitionJiraIssue moves the selected Jira issue to another status (milestone M11).
type TransitionJiraIssue struct {
	Status string `toml:"status"`
}

// SelectGitHubIssue selects a GitHub issue (milestone M11).
type SelectGitHubIssue struct {
	Labels []string `toml:"labels"`
}

// SelectGiteaIssue selects a Gitea issue (milestone M11).
type SelectGiteaIssue struct {
	Labels []string `toml:"labels"`
}

// SelectIssueFromBranch selects the issue named by the current branch (milestone M11).
type SelectIssueFromBranch struct{}

// SwitchBranches switches to the branch of the selected issue (milestone M11).
type SwitchBranches struct{}

// RebaseBranch rebases onto another branch (milestone M11).
type RebaseBranch struct {
	To string `toml:"to"`
}

func (*BumpVersion) Type() string           { return "BumpVersion" }
func (*PrepareRelease) Type() string        { return "PrepareRelease" }
func (*Release) Type() string               { return "Release" }
func (*CreateChangeFile) Type() string      { return "CreateChangeFile" }
func (*CreatePullRequest) Type() string     { return "CreatePullRequest" }
func (*SelectJiraIssue) Type() string       { return "SelectJiraIssue" }
func (*TransitionJiraIssue) Type() string   { return "TransitionJiraIssue" }
func (*SelectGitHubIssue) Type() string     { return "SelectGitHubIssue" }
func (*SelectGiteaIssue) Type() string      { return "SelectGiteaIssue" }
func (*SelectIssueFromBranch) Type() string { return "SelectIssueFromBranch" }
func (*SwitchBranches) Type() string        { return "SwitchBranches" }
func (*RebaseBranch) Type() string          { return "RebaseBranch" }

func (s *BumpVersion) Run(_ context.Context, rt workflow.RunType[workflow.State]) (workflow.RunType[workflow.State], error) {
	return rt, notImplemented(s, "M3")
}
func (s *PrepareRelease) Run(_ context.Context, rt workflow.RunType[workflow.State]) (workflow.RunType[workflow.State], error) {
	return rt, notImplemented(s, "M5")
}
func (s *Release) Run(_ context.Context, rt workflow.RunType[workflow.State]) (workflow.RunType[workflow.State], error) {
	return rt, notImplemented(s, "M6")
}
func (s *CreateChangeFile) Run(_ context.Context, rt workflow.RunType[workflow.State]) (workflow.RunType[workflow.State], error) {
	return rt, notImplemented(s, "M4")
}
func (s *CreatePullRequest) Run(_ context.Context, rt workflow.RunType[workflow.State]) (workflow.RunType[workflow.State], error) {
	return rt, notImplemented(s, "M7")
}
func (s *SelectJiraIssue) Run(_ context.Context, rt workflow.RunType[workflow.State]) (workflow.RunType[workflow.State], error) {
	return rt, notImplemented(s, "M11")
}
func (s *TransitionJiraIssue) Run(_ context.Context, rt workflow.RunType[workflow.State]) (workflow.RunType[workflow.State], error) {
	return rt, notImplemented(s, "M11")
}
func (s *SelectGitHubIssue) Run(_ context.Context, rt workflow.RunType[workflow.State]) (workflow.RunType[workflow.State], error) {
	return rt, notImplemented(s, "M11")
}
func (s *SelectGiteaIssue) Run(_ context.Context, rt workflow.RunType[workflow.State]) (workflow.RunType[workflow.State], error) {
	return rt, notImplemented(s, "M11")
}
func (s *SelectIssueFromBranch) Run(_ context.Context, rt workflow.RunType[workflow.State]) (workflow.RunType[workflow.State], error) {
	return rt, notImplemented(s, "M11")
}
func (s *SwitchBranches) Run(_ context.Context, rt workflow.RunType[workflow.State]) (workflow.RunType[workflow.State], error) {
	return rt, notImplemented(s, "M11")
}
func (s *RebaseBranch) Run(_ context.Context, rt workflow.RunType[workflow.State]) (workflow.RunType[workflow.State], error) {
	return rt, notImplemented(s, "M11")
}

func notImplemented(step workflow.Step, milestone string) error {
	return diag.Newf("Step %s is not implemented yet (milestone %s)", step.Type(), milestone).
		WithCode("remeris::step_not_implemented")
}
