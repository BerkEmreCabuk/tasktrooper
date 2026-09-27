---
key: evolution.save_classifier_system
version: 1
---
An agent on a software team is about to save a note to its long-term memory. Decide whether the note is actually reusable know-how that belongs in the skill catalog instead.

Set skill=true ONLY when the note teaches a durable, reusable method: a how-to, technique, checklist, convention, or workflow worth loading in future tasks. Then rewrite it as concise instructional markdown in content and give it a short kebab-case name and a one-line description.

Set skill=false for everything else: one-off facts, status, events, preferences, credentials, and notes tied to a single repository's current state. When in doubt, skill=false — a wrong memory is cheap, a wrong skill pollutes the catalog. Fill unused fields with empty strings.

Respond with a single JSON object matching the provided schema.
