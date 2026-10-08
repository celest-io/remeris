# Deviations from Knope in ported fixtures

Each entry names a Knope fixture that cannot be ported unchanged, and why (spec: Testing strategy).

| Fixture | Change | Reason |
| --- | --- | --- |
| `validate/kitchen_sink` | Workflow names such as `Cannot SelectJiraIssue without Jira config ` must be renamed (no spaces, no trailing space). | Remeris restricts workflow names to `^[A-Za-z0-9_-]+$` because a `.` would break the koanf key path (spec F3.1). Knope allows any string. |
