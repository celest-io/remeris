# Test data

The `.toml` files are the exact bytes of Knope's golden outputs, which `toml::to_string` printed for the
configuration that `--generate` and `--upgrade` write. `document_test.go` builds the configuration each case implies,
maps it with `Config.Document` and compares the encoded bytes.

| File | Knope source (`crates/knope/tests/`) |
| --- | --- |
| `generate_<case>.toml` | `generate/<case>/out/knope.toml` (`cargo_workspace`, `gitea`, `github`, `no_remote`, `packages`, `packages_with_changelog`) |
| `upgrade_ignore_conventional_commits.toml` | `upgrade/ignore_conventional_commits/out/knope.toml` |
| `upgrade_ignore_conventional_commits.in.toml` | `upgrade/ignore_conventional_commits/in/knope.toml` |

`upgrade/nothing` has no output file: Knope writes nothing when there is nothing to upgrade.

Do not reformat these files: a trailing line feed or a space is part of what is tested.

The fixtures ported from [knope-dev/knope](https://github.com/knope-dev/knope) are © Triax Technologies and Dylan
Anthony, used under the MIT License:

> Copyright (c) 2021 Triax Technologies
> Copyright (c) 2022 Dylan Anthony
>
> Permission is hereby granted, free of charge, to any person obtaining a copy of this software and
> associated documentation files (the "Software"), to deal in the Software without restriction, including
> without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
> copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the
> following conditions:
>
> The above copyright notice and this permission notice shall be included in all copies or substantial
> portions of the Software.
>
> THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT
> LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO
> EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER
> IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR
> THE USE OR OTHER DEALINGS IN THE SOFTWARE.
