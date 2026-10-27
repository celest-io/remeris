# deno.json fixtures

Golden data for `deno_json_test.go`, copied from Knope's `crates/knope/tests/prepare_release/deno/{workspaces,file_priority}` (`in/` as `X.in.json*`, `out/` as `X.out.json*`). They
are © Triax Technologies and Dylan Anthony, used under the MIT License, with the notice in `testdata/README.md` at the repository root.

Changes from Knope's files, all recorded in `testdata/deviations.md`:

- `second.in.json`: the trailing space after `"name": "@scope/second-package",` is removed (whitespace check).
- every `*.out.*`: a final newline is added. Knope writes none (Q10); remeris keeps the final newline of the input (Q6).
