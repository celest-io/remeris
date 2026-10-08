# Deviations from Knope in ported fixtures

Each entry names a Knope fixture that cannot be ported unchanged, and why (spec: Testing strategy).

| Fixture | Change | Reason |
| --- | --- | --- |
| `validate/kitchen_sink` | Workflow names such as `Cannot SelectJiraIssue without Jira config ` must be renamed (no spaces, no trailing space). | Remeris restricts workflow names to `^[A-Za-z0-9_-]+$` because a `.` would break the koanf key path (spec F3.1). Knope allows any string. |
| Version grammar (F1.1) | `1.2.3-rc4`, `1.2.3-rc.4.5`, `1.2.3+build.7` and `1.2.3-alpha.beta.1` are valid versions. Knope rejected them, so tags like these were skipped; they now count as releases. | Full SemVer 2.0.0 (D8). |
| Version grammar (F1.1) | Leading zeros are rejected: `01.2.3` (Knope read it as `1.2.3`) and `1.2.3-rc.01`. Build identifiers may keep leading zeros. | Full SemVer 2.0.0 (D8). |
| Version ordering (F1.1) | Pre-releases are ordered by SemVer 2.0.0 section 11 precedence, not by "label string, then number". Both give the same order for the `label.N` shape. | Quirk Q2, D8. |
| Version display and equality (F1.1) | `+build` metadata is kept when a version is printed or written. Versions that differ only in build metadata have the same precedence but are not equal, so `1.2.3` and `1.2.3+b1` in one package are reported as inconsistent (F7.2). | D8. |
