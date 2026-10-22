package versionedfile

// newCargoLockFile parses Cargo.lock. It is a stub until its milestone lands: replace the body with the real
// parser and delete the notImplemented call. Nothing outside this file needs to change.
func newCargoLockFile(_, _ string) (File, error) {
	return nil, notImplemented(CargoLock, milestoneM9)
}
