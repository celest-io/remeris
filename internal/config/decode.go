package config

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/go-viper/mapstructure/v2"

	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/steps"
	"github.com/celest-io/remeris/internal/variables"
	"github.com/celest-io/remeris/internal/workflow"
)

var (
	stepType          = reflect.TypeFor[workflow.Step]()
	versionedFileType = reflect.TypeFor[VersionedFile]()
	assetsType        = reflect.TypeFor[*Assets]()
	entriesType       = reflect.TypeFor[[]variables.Entry]()
)

// decodeHook handles the values mapstructure cannot decode on its own: the tagged union of steps
// and the fields that accept more than one shape (spec F2.4).
func decodeHook(_, to reflect.Type, data any) (any, error) {
	switch to {
	case stepType:
		return decodeStep(data)
	case versionedFileType:
		return decodeVersionedFile(data)
	case assetsType:
		return decodeAssets(data)
	case entriesType:
		return decodeEntries(data)
	}
	return data, nil
}

// stepConstructors maps the `type` key to a new, empty step.
var stepConstructors = map[string]func() workflow.Step{
	"Command":               func() workflow.Step { return &steps.Command{} },
	"BumpVersion":           func() workflow.Step { return &steps.BumpVersion{} },
	"PrepareRelease":        func() workflow.Step { return &steps.PrepareRelease{} },
	"Release":               func() workflow.Step { return &steps.Release{} },
	"CreateChangeFile":      func() workflow.Step { return &steps.CreateChangeFile{} },
	"CreatePullRequest":     func() workflow.Step { return &steps.CreatePullRequest{} },
	"SelectJiraIssue":       func() workflow.Step { return &steps.SelectJiraIssue{} },
	"TransitionJiraIssue":   func() workflow.Step { return &steps.TransitionJiraIssue{} },
	"SelectGitHubIssue":     func() workflow.Step { return &steps.SelectGitHubIssue{} },
	"SelectGiteaIssue":      func() workflow.Step { return &steps.SelectGiteaIssue{} },
	"SelectIssueFromBranch": func() workflow.Step { return &steps.SelectIssueFromBranch{} },
	"SwitchBranches":        func() workflow.Step { return &steps.SwitchBranches{} },
	"RebaseBranch":          func() workflow.Step { return &steps.RebaseBranch{} },
}

func decodeStep(data any) (any, error) {
	table, ok := data.(map[string]any)
	if !ok {
		return nil, diag.New("Each workflow step must be a table with a 'type' key")
	}
	typ, _ := table["type"].(string)
	newStep, known := stepConstructors[typ]
	if !known {
		names := make([]string, 0, len(stepConstructors))
		for name := range stepConstructors {
			names = append(names, name)
		}
		sort.Strings(names)
		if typ == "" {
			return nil, diag.New("A workflow step is missing its 'type' key").
				WithHelp("Valid step types: " + strings.Join(names, ", "))
		}
		return nil, diag.Newf("Unknown step type %q", typ).
			WithHelp("Valid step types: " + strings.Join(names, ", "))
	}

	step := newStep()
	dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{DecodeHook: decodeHook, Result: step, TagName: tagName})
	if err != nil {
		return nil, err
	}
	if err := dec.Decode(table); err != nil {
		return nil, err
	}
	if bump, ok := step.(*steps.BumpVersion); ok {
		if err := checkBumpVersion(bump); err != nil {
			return nil, err
		}
	}
	return step, nil
}

// checkBumpVersion validates the `rule` of a BumpVersion step. The rule is a second tagged union:
// only Pre takes a label, and it requires one.
func checkBumpVersion(b *steps.BumpVersion) error {
	switch b.Rule {
	case steps.RuleMajor, steps.RuleMinor, steps.RulePatch, steps.RuleRelease:
		b.Label = ""
	case steps.RulePre:
		if b.Label == "" {
			return diag.New("The Pre rule of BumpVersion requires a 'label'")
		}
	default:
		return diag.Newf("Unknown BumpVersion rule %q", string(b.Rule)).
			WithHelp("Valid rules: Major, Minor, Patch, Pre, Release")
	}
	return nil
}

// decodeVersionedFile accepts a bare path, `{path, dependency}` or `{path, regex}`, where regex is
// a string or an array of strings.
func decodeVersionedFile(data any) (any, error) {
	if path, ok := data.(string); ok {
		return VersionedFile{Path: path}, nil
	}
	table, ok := data.(map[string]any)
	if !ok {
		return nil, diag.New("Each entry of 'versioned_files' must be a path or a table")
	}
	path, ok := table["path"].(string)
	if !ok {
		return nil, diag.New("A 'versioned_files' table needs a string 'path'")
	}
	file := VersionedFile{Path: path}
	if dep, present := table["dependency"]; present {
		s, ok := dep.(string)
		if !ok {
			return nil, diag.Newf("The 'dependency' of %s must be a string", path)
		}
		file.Dependency = &s
	}
	switch re := table["regex"].(type) {
	case nil:
	case string:
		file.Regex = []string{re}
	case []string:
		file.Regex = append([]string{}, re...)
	case []any:
		file.Regex = []string{} // `regex = []` is present but empty, which is not the same as absent
		for _, item := range re {
			s, ok := item.(string)
			if !ok {
				return nil, diag.Newf("The 'regex' of %s must be a string or an array of strings", path)
			}
			file.Regex = append(file.Regex, s)
		}
	default:
		return nil, diag.Newf("The 'regex' of %s must be a string or an array of strings", path)
	}
	return file, nil
}

// decodeEntries turns a `variables` table into entries. koanf's maps have no order, so the entries
// come out sorted by key and orderVariables puts them in declaration order afterwards.
func decodeEntries(data any) (any, error) {
	table, ok := data.(map[string]any)
	if !ok {
		return nil, diag.New("'variables' must be a table")
	}
	entries := make([]variables.Entry, 0, len(table)) // non-nil even when empty: `variables = {}` turns the defaults off
	for key, value := range table {
		name, _ := value.(string)
		v := variables.Variable(name)
		if !v.Valid() {
			return nil, diag.New(fmt.Sprintf("Unknown variable %v for %q", value, key)).
				WithHelp("Valid variables: Version, ChangelogEntry, IssueBranch")
		}
		entries = append(entries, variables.Entry{Key: key, Variable: v})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return entries, nil
}

// orderedPackages lists packages in the order the file declared them. keys comes from the index.
func orderedPackages(packages map[string]Package, keys []string) []NamedPackage {
	var out []NamedPackage
	for _, name := range keys {
		if pkg, ok := packages[name]; ok {
			out = append(out, NamedPackage{Name: name, Package: pkg})
			delete(packages, name)
		}
	}
	// Anything the index missed (it should not) still has to be kept; sort for determinism.
	rest := make([]string, 0, len(packages))
	for name := range packages {
		rest = append(rest, name)
	}
	sort.Strings(rest)
	for _, name := range rest {
		out = append(out, NamedPackage{Name: name, Package: packages[name]})
	}
	return out
}

// orderVariables restores declaration order in the variables of a step. path locates the step in
// the source, as in tomlIndex.at.
func orderVariables(step workflow.Step, index *tomlIndex, path ...any) {
	keysOf := func(more ...any) []string { return index.Keys(append(slices.Clone(path), more...)...) }
	switch s := step.(type) {
	case *steps.Command:
		s.Variables = reorder(s.Variables, keysOf("variables"))
	case *steps.CreatePullRequest:
		s.Title.Variables = reorder(s.Title.Variables, keysOf("title", "variables"))
		s.Body.Variables = reorder(s.Body.Variables, keysOf("body", "variables"))
	}
}

// reorder sorts entries by the position of their key in keys. Entries whose key is not listed
// stay at the end in their current order.
func reorder(entries []variables.Entry, keys []string) []variables.Entry {
	position := func(key string) int {
		if i := slices.Index(keys, key); i >= 0 {
			return i
		}
		return len(keys)
	}
	sort.SliceStable(entries, func(i, j int) bool { return position(entries[i].Key) < position(entries[j].Key) })
	return entries
}

// decodeAssets reads `assets`: a string is a glob pattern, anything else is decoded as a list of entries.
func decodeAssets(data any) (any, error) {
	switch v := data.(type) {
	case string:
		return map[string]any{"IsGlob": true, "Glob": v}, nil
	case []any, []map[string]any:
		return map[string]any{"List": v}, nil
	}
	return data, nil
}
