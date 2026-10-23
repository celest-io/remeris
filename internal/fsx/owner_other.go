//go:build !unix

package fsx

import "os"

// restoreOwner does nothing where files have no numeric owner.
func restoreOwner(*os.File, os.FileInfo) {}
