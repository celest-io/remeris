// Package denolock is the model, the reader and the printer of a version 5 deno.lock: the part of the Rust crate
// deno_lockfile 0.49.0 that Knope uses to write the lockfile (LockfileContent::from_json, then
// Lockfile::as_json_string). It is pure: no I/O, environment or clock. Rewriting a lockfile for a new version of a
// package, which is Knope's algorithm in versioned_file/deno_lock.rs, is not here; it works on the model.
//
// The code is ported from deno_lockfile (MIT licence, Copyright 2018-2026 the Deno authors): src/lib.rs (from_json, the
// types it fills and their serde derives, as_json_string) and src/printer.rs (print_v5_content and its Formatter).
//
// Reading and printing:
//
//   - ParseJSON reads the text into an ordered JSON tree, like serde_json::from_str into a Value. Version reports the
//     top-level "version" so that the caller can refuse anything but "5" with its own messages: the model reads
//     version 5 only (versions 1 to 4 need npm package metadata that Knope does not fetch).
//   - FromJSON (or Parse, which is ParseJSON and FromJSON) builds a Content. It keeps the fields that deno_lockfile
//     keeps and drops the rest, so unknown top-level keys, and "workspace" keys it does not know, are lost.
//     The parsers of denosemver read the requirements: the loose grammar for the keys of "specifiers", the strict one
//     for the dependencies of jsr packages, the normalised one for the dependency lists of the workspace. Each
//     error has the Display text of the Rust error (DeserializationError); the serde messages for a section of the
//     wrong shape ("invalid type: integer `5`, expected a map") are reproduced for the common cases.
//   - Content.Print writes the canonical text, with the final newline; PrintContent leaves it out. It does not read
//     the original formatting: it is a function of the model alone, so Print(Parse(x)) is x when x is already
//     canonical, and Print(Parse(Print(Parse(x)))) is Print(Parse(x)) always.
//
// Rewriting. Content is made to be edited in place:
//
//   - a version of a requirement: Content.Specifiers[i].Resolved (and .Req for the requirement itself);
//   - a registry key: Content.JSR[i].Nv or Content.NPM[i].ID. Print sorts and keeps one entry per key, the last one
//     winning, so renaming two packages onto one key is a replacement, like removing a JSON key and inserting it again;
//   - the workspace: Content.Workspace.Root, .Members[path] and .Links[key] hold the requirement lists; a link is
//     renamed by moving its map entry. Their lists are sets: Print sorts and de-duplicates them.
//
// Differences from the Rust code, all in cases that a lockfile written by Deno does not reach:
//
//   - A struct read from a JSON array is an error here ("invalid type: sequence"); serde reads the array positionally.
//   - Which specifier a bare "jsr:name" dependency resolves to when the name has several specifiers is
//     unspecified in Rust (it walks a HashMap); here it is the first in sorted order.
//   - extract_nv_from_id slices value[1..] in Rust, which panics when the first character is not ASCII; the Go
//     version searches bytes and never panics.
//   - JSON syntax errors have the text of Go's JSON reader, not serde_json's.
//   - Floats in free-form JSON print in the serde_json style shared with jsonedit (1e+16); that has not been checked
//     against the serde_json version Knope pins.
//
// The first, second and fourth items are also recorded in testdata/deviations.md at the root of the repository.
package denolock
