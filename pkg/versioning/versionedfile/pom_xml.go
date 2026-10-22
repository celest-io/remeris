package versionedfile

// newMavenPomFile parses pom.xml. It is a stub until its milestone lands: replace the body with the real
// parser and delete the notImplemented call. Nothing outside this file needs to change.
func newMavenPomFile(_, _ string) (File, error) {
	return nil, notImplemented(MavenPom, milestoneM9)
}
