package config

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const regularName = "regular.toml"

func TestReadManifestFile(t *testing.T) {
	cases := map[string]struct {
		setup   func(t *testing.T)
		path    string
		wantLen int
		wantIs  error
	}{
		"regular file": {
			setup:   func(t *testing.T) { writeTemp(t, regularName, "a = 1\n") },
			path:    regularName,
			wantLen: 6,
		},
		"missing": {setup: func(*testing.T) {}, path: "nope.toml", wantIs: fs.ErrNotExist},
		"directory": {
			setup:  func(t *testing.T) { mkdirTemp(t, "dir") },
			path:   "dir",
			wantIs: errNotRegularFile,
		},
		"symlink to a regular file": {
			setup: func(t *testing.T) {
				writeTemp(t, regularName, "a = 1\n")
				if err := os.Symlink(regularName, "link.toml"); err != nil {
					t.Skipf("no symlinks: %v", err)
				}
			},
			path:    "link.toml",
			wantLen: 6,
		},
		"exactly the cap": {
			setup:   func(t *testing.T) { writeTemp(t, regularName, strings.Repeat("#", maxManifestSize)) },
			path:    regularName,
			wantLen: maxManifestSize,
		},
		"over the cap": {
			setup:  func(t *testing.T) { writeTemp(t, regularName, strings.Repeat("#", maxManifestSize+1)) },
			path:   regularName,
			wantIs: errManifestTooLarge,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			tc.setup(t)
			data, err := readManifestFile(osRoot{}, tc.path)
			if tc.wantIs != nil {
				if !errors.Is(err, tc.wantIs) {
					t.Fatalf("error = %v, want %v", err, tc.wantIs)
				}
				if bytes.Contains([]byte(err.Error()), []byte("##")) {
					t.Errorf("the error carries file content")
				}
				return
			}
			if err != nil || len(data) != tc.wantLen {
				t.Fatalf("read %d bytes, %v; want %d", len(data), err, tc.wantLen)
			}
		})
	}
}

func writeTemp(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mkdirTemp(t *testing.T, name string) {
	t.Helper()
	if err := os.MkdirAll(name, 0o750); err != nil {
		t.Fatal(err)
	}
}
