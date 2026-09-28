---
name: no-secrets-in-the-repo
priority: 100
enabled: true
---
Never write a credential, token, key, certificate or connection string into the repository, a log, a board comment or a pull request — not in a .env, not in a manifest, not temporarily. Wire a reference to the platform's secret store and document the variable NAME and the shape of its value. A secret found already committed is reported on the card as a finding that requires rotation: deleting the line does not remove it from git history.
