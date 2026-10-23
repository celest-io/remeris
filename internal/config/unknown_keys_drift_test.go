package config

import (
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/variables"
)

// tagKeys are the `toml:"name"` keys of a struct, without the options after a comma and without `-`.
func tagKeys(typ reflect.Type) []string {
	var keys []string
	for i := range typ.NumField() {
		name, _, _ := strings.Cut(typ.Field(i).Tag.Get(tagName), ",")
		if name != "" && name != "-" {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	return keys
}

func schemaKeys(s *schema) []string {
	var keys []string
	if s != nil {
		for k := range s.keys {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// sameKeys fails with the name of every key the structs read and the schema lacks, and the other way round.
func sameKeys(t *testing.T, path string, want []string, s *schema) {
	t.Helper()
	got := schemaKeys(s)
	for _, k := range want {
		if !slices.Contains(got, k) {
			t.Errorf("%s: add %q to the schema in unknown_keys.go", path, k)
		}
	}
	for _, k := range got {
		if !slices.Contains(want, k) {
			t.Errorf("%s: %q is in the schema in unknown_keys.go but no struct reads it", path, k)
		}
	}
}

// checkType compares the schema s of the value of type typ, found at path, with the tags of typ.
func checkType(t *testing.T, path string, typ reflect.Type, s *schema) {
	t.Helper()
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch {
	case typ == reflect.TypeFor[[]variables.Entry]():
		if s == nil || !s.free {
			t.Errorf("%s: variables are free-form: mark the schema free in unknown_keys.go", path)
		}
	case typ == reflect.TypeFor[[]VersionedFile]():
		// Decoded by hand from a path or a table.
		sameKeys(t, path+"[]", []string{"dependency", "path", "regex"}, elemOf(t, path, s))
	case typ == reflect.TypeFor[Assets]():
		// A glob string or a list of entries.
		sameKeys(t, path+"[]", tagKeys(reflect.TypeFor[Asset]()), elemOf(t, path, s))
	case typ.Kind() == reflect.Slice && typ.Elem().Kind() == reflect.Struct:
		checkType(t, path+"[]", typ.Elem(), elemOf(t, path, s))
	case typ.Kind() == reflect.Map:
		if s == nil || s.each == nil {
			t.Errorf("%s: add an `each` schema in unknown_keys.go", path)
			return
		}
		checkType(t, path+".<name>", typ.Elem(), s.each)
	case typ.Kind() == reflect.Struct:
		sameKeys(t, path, tagKeys(typ), s)
		for i := range typ.NumField() {
			f := typ.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get(tagName), ",")
			if name == "" || name == "-" || s == nil || s.keys[name] == nil {
				continue
			}
			checkType(t, joinPath(path, name), f.Type, s.keys[name])
		}
	}
}

func elemOf(t *testing.T, path string, s *schema) *schema {
	t.Helper()
	if s == nil || s.elem == nil {
		t.Errorf("%s: add an `elem` schema in unknown_keys.go", path)
		return nil
	}
	return &schema{keys: s.elem.keys}
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// TestSchemaMatchesTheDecodeStructs fails when a key is added to (or removed from) the structs the loader decodes
// without the schema of UnknownKeys following: --upgrade would refuse valid files.
func TestSchemaMatchesTheDecodeStructs(t *testing.T) {
	t.Parallel()

	// The workflows are checked here because the keys of a step depend on its type.
	rawType := reflect.TypeFor[raw]()
	sameKeys(t, "", tagKeys(rawType), topSchema)
	for i := range rawType.NumField() {
		name, _, _ := strings.Cut(rawType.Field(i).Tag.Get(tagName), ",")
		if name == "workflows" {
			continue
		}
		checkType(t, name, rawType.Field(i).Type, topSchema.keys[name])
	}

	// workflows: the keys of rawWorkflow are the ones UnknownKeys lets through.
	want := []string{"help_text", "name", "steps"}
	if got := tagKeys(reflect.TypeFor[rawWorkflow]()); !slices.Equal(got, want) {
		t.Errorf("workflows: the keys of a workflow are %q: update unknown_keys.go (unknownWorkflowKeys)", got)
	}

	for typeName, construct := range stepConstructors {
		typ := reflect.TypeOf(construct()).Elem()
		sch, listed := stepSchemas[typeName]
		if !listed {
			sch = &schema{}
		}
		sameKeys(t, "steps."+typeName, tagKeys(typ), sch)
		checkType(t, "steps."+typeName, typ, sch)
	}
	for typeName := range stepSchemas {
		if _, ok := stepConstructors[typeName]; !ok {
			t.Errorf("steps.%s is in the schema in unknown_keys.go but is not a step type", typeName)
		}
	}
}
