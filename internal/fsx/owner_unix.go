//go:build unix

package fsx

import (
	"os"
	"syscall"
)

// restoreOwner gives f the owner and group of the file it replaces. It is best effort: an unprivileged process can
// only keep its own user and the groups it belongs to, and the error is of no interest.
func restoreOwner(f *os.File, info os.FileInfo) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		_ = f.Chown(int(st.Uid), int(st.Gid))
	}
}
