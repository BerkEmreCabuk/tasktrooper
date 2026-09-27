---
key: catalog.merge_skill_user
version: 1
inputs: [Name, Local, Upstream]
---
Skill: {{.Name}}
This machine's current copy (LOCAL):
---
{{.Local}}
---
New catalog revision (UPSTREAM):
---
{{.Upstream}}
---

