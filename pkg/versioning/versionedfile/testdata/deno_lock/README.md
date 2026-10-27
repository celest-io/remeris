# deno.lock goldens

`workspaces.*` and `file_priority.*` are the `deno.lock` input and expected output of Knope's `crates/knope/tests/prepare_release/deno/{workspaces,file_priority}` fixtures (the
same files as in `testdata/script/prepare_release/deno_workspaces.txtar` and `deno_file_priority.txtar`). They are copyright (c) 2021 Triax Technologies and (c) 2022 Dylan Anthony,
used under the MIT License (see `testdata/README.md`).

- `workspaces`: `@scope/first-package` is set to 1.1.0, then `@scope/second-package` to 0.1.1.
- `file_priority`: `@scope/test-package` is set to 1.0.1.
