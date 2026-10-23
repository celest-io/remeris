package denolock

import "github.com/celest-io/remeris/pkg/versioning/internal/denosemver"

// Content is the content of a version 5 deno.lock: the fields deno_lockfile 0.49.0 keeps (LockfileContent) and
// nothing else. Top-level keys it does not know are dropped when reading, and so are the keys that Print never
// writes.
//
// Reading puts every slice and map in the order Print writes it; Print sorts again, so the order of a slice you edit
// does not matter. Where a collection is a set or a map in deno_lockfile (a HashSet, a HashMap or a BTreeMap), Print
// keeps one entry per key and the last entry of the slice wins.
type Content struct {
	// Specifiers maps each requirement ("jsr:@std/path@^1.0.0") to the version it resolved to. It is a HashMap in
	// deno_lockfile; Print sorts it by the normalised requirement text.
	Specifiers []Specifier
	// JSR lists the JSR packages, a BTreeMap keyed by PackageNv in deno_lockfile; Print sorts it by PackageNv.Compare.
	JSR []JSRPackage
	// NPM lists the npm packages, a BTreeMap keyed by the package id ("chalk@5.0.0_peer@1"); Print sorts it by ID.
	NPM []NPMPackage
	// Redirects maps a URL to the URL it redirects to.
	Redirects map[string]string
	// Remote maps a URL to its checksum.
	Remote map[string]string
	// Workspace is the "workspace" section.
	Workspace Workspace
}

// Specifier is one entry of the "specifiers" section.
type Specifier struct {
	// Req is the requirement, parsed from the key with the loose grammar. Print writes Req.Normalized(), so
	// "jsr:@std/path@^1.0.0" becomes "jsr:@std/path@1". Req.Req.VersionReq.VersionText() is the text as written.
	Req denosemver.JsrDepPackageReq
	// Resolved is the value: the version the requirement resolved to ("1.0.0"; an npm peer suffix such as
	// "12.0.0_react@18.0.0" is part of the text). It is any string.
	Resolved string
}

// JSRPackage is one entry of the "jsr" section.
type JSRPackage struct {
	Nv        denosemver.PackageNv
	Integrity string
	// Dependencies are the requirements of the package. Reading resolves each dependency of the file to the
	// requirement of the Specifiers entry it points to, so the values are requirements that appear in Specifiers.
	// Print writes a bare "jsr:@std/path" when that name has a single specifier (in either registry) and the
	// normalised requirement otherwise. The slice is a set: duplicates are written once.
	Dependencies []denosemver.JsrDepPackageReq
}

// NPMPackage is one entry of the "npm" section.
type NPMPackage struct {
	// ID is the key: "name@version", plus an optional "_peer@version" suffix. It always contains an '@' after
	// its first character.
	ID string
	// Integrity is nil for the patch packages that have none.
	Integrity *string
	// Dependencies, OptionalDependencies and OptionalPeers are maps from the name a package uses to the id it
	// resolves to, sorted by name. Print writes a bare name when the id has the same name and that name has one
	// version in NPM, "name@version" when it has several, and "alias@npm:name@version" when the names differ.
	Dependencies         []NPMDep
	OptionalDependencies []NPMDep
	OptionalPeers        []NPMDep
	// OS and CPU keep the order of the file; Print writes them on one line.
	OS  []string
	CPU []string
	// Tarball is nil when absent.
	Tarball    *string
	Deprecated bool
	Scripts    bool
	Bin        bool
}

// NPMDep is one dependency of an npm package.
type NPMDep struct {
	// Name is the name the dependent package uses (the alias, for "alias@npm:real@1.0.0").
	Name string
	// ID is the resolved package id: "real@1.0.0" or "real@1.0.0_peer@1".
	ID string
}

// Workspace is the "workspace" section: the dependencies of the root deno.json and package.json, those of each
// member, the links, and the npm overrides.
type Workspace struct {
	// Root holds the dependencies of the workspace root (the keys "dependencies" and "packageJson" of the section).
	Root WorkspaceMember
	// NpmOverrides is the npm overrides of the root package.json, nil when there are none. Print writes it as
	// "packageJson.overrides" of the root. Reading takes it from "npmOverrides", or else from "packageJson.overrides".
	NpmOverrides *Value
	// Members maps the member path ("packages/foo") to its dependencies.
	Members map[string]*WorkspaceMember
	// Links maps a link key such as "jsr:@scope/pkg@1.0.0" to the link. Print writes the key as it is.
	Links map[string]*Link
}

// WorkspaceMember holds the dependency requirements of one workspace member.
type WorkspaceMember struct {
	// Dependencies are the requirements of the deno.json of the member ("dependencies"). A set: Print sorts it by
	// the normalised text and writes each requirement once.
	Dependencies []denosemver.JsrDepPackageReq
	// PackageJSONDependencies are the requirements of its package.json ("packageJson.dependencies"). A set.
	PackageJSONDependencies []denosemver.JsrDepPackageReq
}

// Link is the content of one entry of "workspace.links".
type Link struct {
	// Dependencies, OptionalDependencies and PeerDependencies are sets of requirements.
	Dependencies         []denosemver.JsrDepPackageReq
	OptionalDependencies []denosemver.JsrDepPackageReq
	PeerDependencies     []denosemver.JsrDepPackageReq
	// PeerDependenciesMeta is free-form JSON per package, kept as it was read.
	PeerDependenciesMeta map[string]Value
}
