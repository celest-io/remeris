// Package versionedfile is the common model of files that hold a version (spec F6.1): formats, configuration,
// the File interface and the errors shared by all formats. It does no I/O: callers pass the file content in
// and execute the returned actions.
package versionedfile

import "strings"

// Format is a kind of versioned file.
type Format int

// The supported formats.
const (
	Cargo Format = iota
	CargoLock
	DenoJson
	DenoLock
	Gleam
	GoMod
	PackageJson
	PackageLockJson
	MavenPom
	PubSpec
	PyProject
	TauriConf
	RegexFile
)

var formatNames = [...]string{
	Cargo: "Cargo", CargoLock: "CargoLock", DenoJson: "DenoJson", DenoLock: "DenoLock", Gleam: "Gleam", GoMod: "GoMod",
	PackageJson: "PackageJson", PackageLockJson: "PackageLockJson", MavenPom: "MavenPom", PubSpec: "PubSpec",
	PyProject: "PyProject", TauriConf: "TauriConf", RegexFile: "RegexFile",
}

// String returns the format's identifier, such as "Cargo".
func (f Format) String() string {
	if f < 0 || int(f) >= len(formatNames) {
		return "Unknown"
	}
	return formatNames[f]
}

// FileNames lists the supported file names, matched exactly on the last path component. The order is also the
// order of default detection (F20). deno.jsonc is added on top of Knope's table (Q11).
var FileNames = []struct {
	Name   string
	Format Format
}{
	{"Cargo.toml", Cargo}, {"Cargo.lock", CargoLock}, {"deno.json", DenoJson}, {"deno.jsonc", DenoJson},
	{"deno.lock", DenoLock}, {"gleam.toml", Gleam}, {"go.mod", GoMod}, {"package.json", PackageJson},
	{"package-lock.json", PackageLockJson}, {"pom.xml", MavenPom}, {"pubspec.yaml", PubSpec},
	{"pyproject.toml", PyProject}, {"tauri.conf.json", TauriConf}, {"tauri.macos.conf.json", TauriConf},
	{"tauri.windows.conf.json", TauriConf}, {"tauri.linux.conf.json", TauriConf},
}

// baseName returns the last component of a slash-separated path.
func baseName(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// FormatFromPath returns the format for a slash-separated path, from its last component (case-sensitive).
func FormatFromPath(path string) (Format, bool) {
	name := baseName(path)
	for _, f := range FileNames {
		if f.Name == name {
			return f.Format, true
		}
	}
	return 0, false
}

// SupportsDependency reports whether a versioned_files entry of this format may name a dependency (F6.3).
func (f Format) SupportsDependency() bool {
	switch f {
	case Cargo, CargoLock, DenoJson, DenoLock, PackageJson, PackageLockJson:
		return true
	default:
		return false
	}
}

// GoVersioning controls how go.mod handles major versions.
type GoVersioning int

// The go.mod versioning modes.
const (
	// Standard applies Go's module rules: a major version above 1 changes the module path.
	Standard GoVersioning = iota
	// IgnoreMajorRules skips those rules (ignore_go_major_versioning).
	IgnoreMajorRules
	// BumpMajor allows a major bump even when the rules would forbid it (forced version overrides).
	BumpMajor
)
