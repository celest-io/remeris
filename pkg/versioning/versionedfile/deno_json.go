package versionedfile

// newDenoJSONFile parses deno.json and deno.jsonc. It is a stub until its milestone lands: replace the body with the real
// parser and delete the notImplemented call. Nothing outside this file needs to change.
func newDenoJSONFile(_, _ string) (File, error) {
	return nil, notImplemented(DenoJson, milestoneM9)
}
