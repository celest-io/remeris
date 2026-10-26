package versionedfile

import "testing"

// TestWarn sets the package-level sink, so it must not run in parallel.
func TestWarn(t *testing.T) {
	warn("dropped without a sink")

	var got []string
	SetWarnSink(func(msg string) { got = append(got, msg) })
	t.Cleanup(func() { SetWarnSink(nil) })

	warn("Cargo.lock is out of date")
	if len(got) != 1 || got[0] != "Cargo.lock is out of date" {
		t.Errorf("got %q, want the one warning", got)
	}

	SetWarnSink(nil)
	warn("dropped again")
	if len(got) != 1 {
		t.Errorf("got %q after the sink was removed", got)
	}
}
