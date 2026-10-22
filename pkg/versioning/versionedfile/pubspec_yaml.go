package versionedfile

// newPubSpecFile parses pubspec.yaml. It is a stub until its milestone lands: replace the body with the real
// parser and delete the notImplemented call. Nothing outside this file needs to change.
func newPubSpecFile(_, _ string) (File, error) {
	return nil, notImplemented(PubSpec, milestoneM9)
}
