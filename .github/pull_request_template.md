## Summary

<!-- What changes and why. The pull request title is a conventional commit message (it becomes the squashed commit). -->

## Related issue

<!-- Closes #123, or "none" for a typo or a small fix. -->

## Tests

<!-- Unit tests, golden tests (testdata/script) or fixtures added or changed, and what you ran. -->

## Checklist

- [ ] The title is a conventional commit message (`feat`, `fix`, `chore`, `ci`, `docs`, `refactor`, `test`, `style`), lowercase and imperative.
- [ ] `mise run ci` passes locally.
- [ ] `docs/spec.md` and the other docs are updated when the behaviour changes.
- [ ] `testdata/deviations.md` (and `docs/differences.md` when users can notice it) is updated if the behaviour differs from Knope.
- [ ] I did not edit `CHANGELOG.md` or `VERSION`.
- [ ] No secrets, tokens or private addresses are in the change, the tests or the description.
