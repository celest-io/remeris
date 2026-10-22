# Test data of denolock

## `specs/`

The spec files of deno_lockfile 0.49.0 (`tests/specs/config_changes/**` and `tests/specs/transforms/*`), copied
unchanged from the crate published on crates.io: <https://crates.io/crates/deno_lockfile/0.49.0>.

Each file is a list of sections that start with a `# title` line. The first is a lockfile; after it, each section that
describes a change is followed by the lockfile Deno wrote for it. `spec_test.go` reads the lockfiles that have
`"version": "5"` (the outputs of Deno's printer and the hand-written version 5 inputs) and prints them again with
`Print(Parse(x))`; the outputs must come back byte for byte.

deno_lockfile is MIT licensed:

```text
MIT License

Copyright (c) 2018-2026 the Deno authors

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated
documentation files (the "Software"), to deal in the Software without restriction, including without limitation the
rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit
persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the
Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE
WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR
OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
```

## `knope/`

The `deno.lock` files of Knope's fixtures `crates/knope/tests/prepare_release/deno/{workspaces,file_priority}/{in,out}`
(Knope is MIT licensed, copyright its authors): <https://github.com/knope-dev/knope>. The `out` files are what
Knope writes with deno_lockfile's printer, so they round-trip byte for byte.
