package config

import (
	"strconv"
	"strings"
)

// schema describes the keys remeris reads in a table. The loader ignores the others (Quirk Q1), so a configuration
// written back from the loaded values would lose them.
type schema struct {
	keys map[string]*schema // the known keys of a table
	each *schema            // any key of a table has this schema (`[packages.<name>]`)
	elem *schema            // the schema of the elements of an array
	free bool               // any key below is accepted (`variables`)
}

// keyName is the key `name` of extra changelog sections and assets.
const keyName = "name"

var (
	leaf    = &schema{}
	freeTab = &schema{free: true}

	templateSchema = &schema{keys: map[string]*schema{"template": leaf, "variables": freeTab}}

	packageSchema = &schema{keys: map[string]*schema{
		"versioned_files": {
			elem: &schema{keys: map[string]*schema{"path": leaf, "dependency": leaf, "regex": leaf}},
		},
		"changelog":                  leaf,
		"scopes":                     leaf,
		"extra_changelog_sections":   {elem: &schema{keys: map[string]*schema{keyName: leaf, "footers": leaf, "types": leaf}}},
		"assets":                     {elem: &schema{keys: map[string]*schema{"path": leaf, keyName: leaf}}},
		"ignore_go_major_versioning": leaf,
	}}

	// stepSchemas are the keys of each step type besides `type`. A type that is not listed has no other key.
	stepSchemas = map[string]*schema{
		"Command":     {keys: map[string]*schema{"command": leaf, "variables": freeTab, "shell": leaf}},
		"BumpVersion": {keys: map[string]*schema{"rule": leaf, "label": leaf}},
		"PrepareRelease": {
			keys: map[string]*schema{"prerelease_label": leaf, "allow_empty": leaf, "ignore_conventional_commits": leaf},
		},
		"CreatePullRequest":   {keys: map[string]*schema{"base": leaf, "title": templateSchema, "body": templateSchema}},
		"SelectJiraIssue":     {keys: map[string]*schema{"status": leaf}},
		"TransitionJiraIssue": {keys: map[string]*schema{"status": leaf}},
		"SelectGitHubIssue":   {keys: map[string]*schema{"labels": leaf}},
		"SelectGiteaIssue":    {keys: map[string]*schema{"labels": leaf}},
		"RebaseBranch":        {keys: map[string]*schema{"to": leaf}},
	}

	topSchema = &schema{keys: map[string]*schema{
		"changes":       {keys: map[string]*schema{"ignore_conventional_commits": leaf}},
		"release_notes": {keys: map[string]*schema{"change_templates": leaf}},
		"package":       packageSchema,
		"packages":      {each: packageSchema},
		"workflows":     nil, // walked by hand: the keys of a step depend on its type
		"github":        {keys: map[string]*schema{"owner": leaf, "repo": leaf, "api_url": leaf, "upload_url": leaf}},
		"gitea":         {keys: map[string]*schema{"owner": leaf, "repo": leaf, "host": leaf}},
		"jira":          {keys: map[string]*schema{"url": leaf, "project": leaf}},
	}}
)

// UnknownKeys lists, in declaration order, the keys of the loaded file that remeris does not read, as paths such as
// `changes.ignore_conventional_merge_commits` or `workflows[0].steps[1].foo`. A configuration written back from the
// loaded values (--upgrade) would drop them. It is empty for a configuration that was not read from a file.
func (c *Config) UnknownKeys() []string {
	if c == nil || c.index == nil || c.index.root == nil {
		return nil
	}
	var out []string
	for _, key := range c.index.root.keys {
		kid := c.index.root.kids[key]
		sub, known := topSchema.keys[key]
		switch {
		case !known:
			out = append(out, keyPath("", key))
		case key == "workflows":
			out = c.unknownWorkflowKeys(out, kid)
		default:
			out = walkKeys(out, kid, sub, keyPath("", key))
		}
	}
	return out
}

func (c *Config) unknownWorkflowKeys(out []string, workflows *indexNode) []string {
	for i, wf := range workflows.elems {
		path := "workflows[" + strconv.Itoa(i) + "]"
		for _, key := range wf.keys {
			switch kid := wf.kids[key]; key {
			case keyName, "help_text":
			case "steps":
				for j, step := range kid.elems {
					stepPath := path + ".steps[" + strconv.Itoa(j) + "]"
					sch := &schema{}
					if i < len(c.Workflows) && j < len(c.Workflows[i].Steps) {
						sch = stepSchemas[c.Workflows[i].Steps[j].Type()]
					}
					for _, k := range step.keys {
						if k == "type" {
							continue
						}
						if sub, ok := sch.keysOf(k); ok {
							out = walkKeys(out, step.kids[k], sub, keyPath(stepPath, k))
						} else {
							out = append(out, keyPath(stepPath, k))
						}
					}
				}
			default:
				out = append(out, keyPath(path, key))
			}
		}
	}
	return out
}

func (s *schema) keysOf(key string) (*schema, bool) {
	if s == nil {
		return nil, false
	}
	sub, ok := s.keys[key]
	return sub, ok
}

// walkKeys appends the paths of the keys of n that s does not list, recursively. Array elements are checked with
// the element schema.
func walkKeys(out []string, n *indexNode, s *schema, path string) []string {
	if n == nil || s == nil || s.free {
		return out
	}
	if s.elem != nil {
		for i, e := range n.elems {
			out = walkKeys(out, e, &schema{keys: s.elem.keys}, path+"["+strconv.Itoa(i)+"]")
		}
		return out
	}
	for _, key := range n.keys {
		sub, ok := s.keys[key]
		if !ok && s.each != nil {
			sub, ok = s.each, true
		}
		if !ok {
			out = append(out, keyPath(path, key))
			continue
		}
		out = walkKeys(out, n.kids[key], sub, keyPath(path, key))
	}
	return out
}

// keyPath appends key to path, quoting it when it is not a bare TOML key.
func keyPath(path, key string) string {
	if key == "" || strings.IndexFunc(key, func(r rune) bool {
		isBare := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-'
		return !isBare
	}) >= 0 {
		key = strconv.Quote(key)
	}
	if path == "" {
		return key
	}
	return path + "." + key
}
