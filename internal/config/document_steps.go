package config

import (
	"fmt"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/steps"
	"github.com/celest-io/remeris/internal/tomlwrite"
	"github.com/celest-io/remeris/internal/variables"
	"github.com/celest-io/remeris/internal/workflow"
)

// workflowTable maps a workflow: `name`, `help_text` unless it is empty, then the `[[workflows.steps]]`. An empty
// list of steps is written as `steps = []`.
func workflowTable(name, helpText string, list []workflow.Step) (*tomlwrite.Table, error) {
	t := tomlwrite.NewTable()
	t.Set("name", tomlwrite.String(name))
	setNonEmpty(t, "help_text", helpText)
	tables := make([]*tomlwrite.Table, 0, len(list))
	for _, step := range list {
		st, err := stepTable(step)
		if err != nil {
			return nil, diag.Newf("Cannot write workflow %s", name).WithCode(codeDocumentStep).WithRelated(err)
		}
		tables = append(tables, st)
	}
	t.Set("steps", tomlwrite.NewArrayOfTables(tables...))
	return t, nil
}

// stepTable maps one step to its table, with `type` first and then the fields in the order of Knope's Step enum.
// Every type that stepConstructors can build has a case here (TestEveryStepTypeHasASerializer fails otherwise).
// Fields follow Knope's serde attributes.
func stepTable(step workflow.Step) (*tomlwrite.Table, error) {
	if step == nil {
		return nil, diag.New("A workflow has an empty step").WithCode(codeDocumentStep)
	}
	t := tomlwrite.NewTable()
	t.Set("type", tomlwrite.String(step.Type()))
	switch s := step.(type) {
	case *steps.Command:
		return t, commandFields(t, s)
	case *steps.BumpVersion:
		return t, bumpVersionFields(t, s)
	case *steps.PrepareRelease:
		// Knope omits the label when unset and both flags when false.
		setNonEmpty(t, "prerelease_label", s.PrereleaseLabel)
		setTrue(t, "allow_empty", s.AllowEmpty)
		setTrue(t, "ignore_conventional_commits", s.IgnoreConventionalCommits)
	case *steps.CreatePullRequest:
		t.Set("base", tomlwrite.String(s.Base))
		title, err := templateTable(s.Title)
		if err != nil {
			return nil, err
		}
		body, err := templateTable(s.Body)
		if err != nil {
			return nil, err
		}
		t.Set("title", title)
		t.Set("body", body)
	case *steps.SelectJiraIssue:
		t.Set("status", tomlwrite.String(s.Status))
	case *steps.TransitionJiraIssue:
		t.Set("status", tomlwrite.String(s.Status))
	case *steps.SelectGitHubIssue:
		setLabels(t, s.Labels)
	case *steps.SelectGiteaIssue:
		setLabels(t, s.Labels)
	case *steps.RebaseBranch:
		t.Set("to", tomlwrite.String(s.To))
	case *steps.Release, *steps.CreateChangeFile, *steps.SelectIssueFromBranch, *steps.SwitchBranches:
		// No fields.
	default:
		return nil, diag.Newf("The step type %q cannot be written to a configuration file", step.Type()).
			WithCode(codeDocumentStep).
			WithHelp("This is a bug in remeris: the type has no writer.")
	}
	return t, nil
}

// commandFields writes `command`, `variables` when the key was set and `shell` when true. Knope keeps an explicit
// `shell = false` (it stores an Option); the configuration here cannot tell it from no key, so it is left out.
func commandFields(t *tomlwrite.Table, s *steps.Command) error {
	t.Set("command", tomlwrite.String(s.Command))
	if s.Variables != nil {
		vars, err := variablesTable(s.Variables)
		if err != nil {
			return err
		}
		t.Set("variables", vars)
	}
	setTrue(t, "shell", s.Shell)
	return nil
}

// bumpVersionFields writes `rule`, and `label` for the Pre rule only, which is the one that has one.
func bumpVersionFields(t *tomlwrite.Table, s *steps.BumpVersion) error {
	switch s.Rule {
	case steps.RuleMajor, steps.RuleMinor, steps.RulePatch, steps.RuleRelease:
		t.Set("rule", tomlwrite.String(string(s.Rule)))
	case steps.RulePre:
		if s.Label == "" {
			return diag.New("The Pre rule of BumpVersion requires a 'label'").WithCode(codeDocumentStep)
		}
		t.Set("rule", tomlwrite.String(string(s.Rule)))
		t.Set("label", tomlwrite.String(s.Label))
	default:
		return diag.Newf("Unknown BumpVersion rule %q", string(s.Rule)).WithCode(codeDocumentStep)
	}
	return nil
}

// templateTable writes a Template the way Knope does: `template` and a `variables` table that is always there, with
// the default variables when the template had no key.
func templateTable(tpl variables.Template) (*tomlwrite.Table, error) {
	t := tomlwrite.NewTable()
	t.Set("template", tomlwrite.String(tpl.Text))
	entries := tpl.Variables
	if entries == nil {
		entries = variables.DefaultEntries()
	}
	vars, err := variablesTable(entries)
	if err != nil {
		return nil, err
	}
	t.Set("variables", vars)
	return t, nil
}

// variablesTable is a table of key to variable name, in declaration order. The writer quotes the keys.
func variablesTable(entries []variables.Entry) (*tomlwrite.Table, error) {
	t := tomlwrite.NewTable()
	for _, e := range entries {
		if !e.Variable.Valid() {
			return nil, diag.New(fmt.Sprintf("Unknown variable %q for %q", string(e.Variable), e.Key)).
				WithCode(codeDocumentStep).
				WithHelp("Valid variables: Version, ChangelogEntry, IssueBranch")
		}
		t.Set(e.Key, tomlwrite.String(string(e.Variable)))
	}
	return t, nil
}

// setLabels writes `labels` unless nil (Knope's `None`); an empty list is kept.
func setLabels(t *tomlwrite.Table, labels []string) {
	if labels != nil {
		t.Set("labels", stringArray(labels))
	}
}

// setTrue stores a flag only when it is set, like `skip_serializing_if = "Not::not"`.
func setTrue(t *tomlwrite.Table, key string, v bool) {
	if v {
		t.Set(key, tomlwrite.Bool(true))
	}
}
