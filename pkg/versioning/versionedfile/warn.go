package versionedfile

import "log/slog"

// warn reports a problem with a versioned file that does not stop the release, such as a lock file that cannot be
// updated (F6.2). The pure core does no I/O, so it only logs through slog.Default; the CLI decides where the
// message goes, and tests capture it by replacing the default logger.
func warn(msg string) {
	slog.Warn(msg)
}
