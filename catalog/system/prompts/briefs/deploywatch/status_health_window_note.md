---
key: briefs.deploywatch.status_health_window_note
version: 1
inputs: [HealthLabel, Until]
---
 Production is now running this task's code; watch {{.HealthLabel}} until {{.Until}} — an incident opened before then is this release's.
