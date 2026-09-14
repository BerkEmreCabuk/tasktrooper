# TaskTrooper agent catalog

Seed content for TaskTrooper's agents: one role prompt per role, and the skills
that role starts with.

```
agents/<role>.md                   role definition: the agent's system prompt
skills/<role>/<skill>/SKILL.md    one directory per skill
```

## Seed, not runtime

These files are a **starting point**, not what a running board uses.

On first boot the server imports them into its database. From there each tenant
owns its own copy: skills are edited in the UI, and the self-evolution loop
rewrites them from that tenant's KPIs and run reflections. A skill installed
from this repository will therefore differ — sometimes a lot — from the skill of
the same name on a board that has been running for a while. That is the design,
not drift.

Consumed by `agent-server` as a git submodule at
`internal/application/catalog/seeddata`, read through `//go:embed`.

## Adding a skill

Create `skills/<role>/<name>/SKILL.md`:

```markdown
---
name: api-contract-testing
category: qa
description: API contract testing with real requests
---

# API Contract Testing

Step-by-step instructions the agent follows when it applies this skill.
```

Rules:

- `name` must equal the directory name. The seeder refuses a mismatch rather
  than importing a skill under a name nothing refers to.
- `category` and `description` are required. The description is what the model
  reads when deciding whether to apply the skill, so it is the field that
  decides whether the skill ever runs — write it as a trigger, not a title.
- The body is the instructions themselves. Reusable procedure, not a narrative.

A new file is not live until the role claims it: roles list their skills in
`role_seed.go` in the agent-server repository.

## Layout

One directory per skill, holding `SKILL.md` — the same shape Claude Code and
Cursor discover natively. That is deliberate: the server materialises a
tenant's catalog into a task workspace in exactly this format, so what is
written here and what an agent reads at run time are the same kind of file.
