package versionedfile

import "sync/atomic"

// warnSink holds the function that receives warnings. It is the one package-level state of the core, and it is set
// by the program (SetWarnSink), never by the core itself.
var warnSink atomic.Pointer[func(string)]

// SetWarnSink sets where warn messages go: problems with a versioned file that do not stop the release, such as a
// lock file that cannot be updated (F6.2). The pure core does no I/O, so the program decides: the CLI passes a
// function that logs the message at warn level. Without a sink (the default, or nil) the messages are dropped.
// It is safe to call concurrently with the writes that warn, and the last call wins.
func SetWarnSink(sink func(string)) {
	if sink == nil {
		warnSink.Store(nil)
		return
	}
	warnSink.Store(&sink)
}

// warn reports a problem with a versioned file that does not stop the release.
func warn(msg string) {
	if sink := warnSink.Load(); sink != nil {
		(*sink)(msg)
	}
}
