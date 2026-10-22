package versionedfile

// newPackageLockJSONFile parses package-lock.json. It is a stub until its milestone lands: replace the body with the real
// parser and delete the notImplemented call. Nothing outside this file needs to change.
func newPackageLockJSONFile(_, _ string) (File, error) {
	return nil, notImplemented(PackageLockJson, milestoneM9)
}
