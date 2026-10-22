// Package denosemver is the subset of the deno_semver crate (version 0.9.1) that Knope's deno.lock support uses
// through deno_lockfile 0.49.0: versions, package name and version pairs, version requirements and the
// "jsr:" and "npm:" dependency requirements, with the parsers and printers of the Rust crate. It is pure: no
// I/O, environment or clock.
//
// The code is ported from deno_semver (MIT licence, Copyright 2018-2025 the Deno authors): src/lib.rs, src/range.rs,
// src/range_set_or_tag.rs, src/specifier.rs, src/common.rs, src/npm.rs, src/package.rs and src/jsr.rs. The parser
// combinators come from monch 0.5.0 (MIT licence, Copyright 2018-2024 the Deno authors), src/lib.rs. Parts of
// the crate that deno.lock does not need are left out: ranges intersection, package references with a sub path
// (PackageReqReference, PackageNvReference and their JSR and npm wrappers), serde and the stack strings.
//
// What the lockfile needs from it:
//
//   - ParseJsrDepPackageReq and its loose and normalised variants parse the keys of "specifiers" and the entries
//     of the dependency arrays; JsrDepPackageReq.Normalized prints them the way deno.lock stores them, and
//     JsrDepPackageReq.Compare gives the order of the printed maps.
//   - ParsePackageNv parses the keys of "jsr" and "npm"; PackageNv.Compare gives their order.
//   - VersionReq.VersionText is the raw requirement text, which Knope matches against; VersionReq.Normalized is the
//     printed form ("^1.0.0" becomes "1", "^0.1.0" becomes "0.1").
//   - ParseStandard parses the version Remeris writes. Its error text is "Invalid version".
//
// Errors follow the thiserror types of the crate: Error returns the Display text of the outermost type, errors.Unwrap
// reaches the source, and errors.As with *Failure reaches the message and code snippet of the grammar. The texts
// are the ones of the Rust crate; where a Rust error carries no source (PackageNvParseError), the message is a field.
package denosemver
