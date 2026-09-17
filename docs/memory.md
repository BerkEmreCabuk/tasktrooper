---
title: Memory
description: The four memory scopes, what agents can and cannot save, and where you manage it.
---

Memory is what an agent still needs to know weeks from now, on a task nobody
has written yet — as distinct from a task's comments, which already record
what happened on that one card. TaskTrooper keeps the two separate on
purpose: memory that filled up with run narration would crowd out the facts
actually worth keeping.

## The four scopes

A memory belongs to one of four buckets, decided by two independent
dimensions: whether it is personal to one agent or shared by the whole team,
and whether it applies everywhere or only inside one repository.

| | Applies everywhere (global) | One repository (project) |
|---|---|---|
| **One agent** | *Agent, global* — the agent's own habits and preferences, wherever it works | *Agent, project* — what this agent learned inside this specific repository |
| **Whole team** | *Team, global* — conventions every agent reads, regardless of agent or repository | *Team, project* — this repository's shared facts (its build command, its deploy flow) |

An agent's own run always sees its own memories plus the team's; whether a
project-scoped memory is visible depends on whether a repository is in play
for that run at all. With no repository in context, project memories are
unreachable — a lesson from one repository never quietly leaks into a run on
an unrelated one.

## What gets refused

`save_memory` (and the equivalent check the reflection job applies before
writing memories on its own) refuses content that is anchored to one card
instead of durable across many: a board task key, a PR number, a commit
SHA, a column move like `code_review → ready_for_qa`, or a sentence like
"this task" or "this run". The tool's refusal names the reason and points at
where that content actually belongs — the task's own comments — and, when
there is a durable fact underneath, suggests the version with the card taken
out of it: not "T-28's checks failed on billing" but "this org's GitHub
Actions billing is blocked, so CI check runs fail within seconds with a
billing annotation, and only a human can clear it."

A save that says essentially the same thing as an existing memory in the
same bucket is also refused — not as an error, but as `saved: false` with a
pointer to the memory it duplicates — so asking an agent to remember
something twice does not produce two entries.

## How agents save and search

Every role agent has three inline tools:

- **`save_memory`** — takes the content, an optional category, a `scope`
  (`project` or `global`; left unset it saves to wherever the run already
  is — project-scoped if a repository is in context, global otherwise — and
  `scope=project` with no repository in context is an error rather than a
  silent global write), and `shared` (team vs. personal to this agent).
- **`search_memory`** — defaults to what the agent can currently see (this
  repository plus global, its own plus the team's), and accepts `scope`
  (`all`/`project`/`global`) and `owner` (`all`/`self`/`team`) to narrow that.
- **`delete_memory`** — removes one by id.

Board runs and chat sessions both get a recall pass injected automatically —
up to 8 project-scoped and 8 global-scoped memories, rendered as separate
sections — so an agent does not have to remember to search before it acts.

`save_memory`'s description spells out the test an agent is meant to apply
itself before calling it: if a note stops being true once the current task
is finished, it is not a memory. It also draws the line to skills for the
agent: reusable know-how — a procedure worth following again — is a skill,
not a memory, and the tool tries to route content like that to the skill
catalog automatically instead of saving it as a memory at all. When that
automatic classification decides the content is reusable know-how, the tool
answers `saved: false` with the skill name it created instead.

## Managing memory in the UI

- **Agent Memory** (the agent's own **Memory** tab) shows that one agent's
  memories, with a scope filter (all / global / per repository), and forms to
  add, edit, or delete an entry by hand.
- **Team Memory** (linked from the sidebar) shows the same manager in
  `shared` mode — memories with no owning agent, visible to the whole team.
  From here you can also **plan a promotion**: scan team memories for ones
  worth turning into a skill, review the proposed skill name, description and
  content per candidate, and apply the ones you approve — each approved
  candidate becomes a skill on the listed agents and the memory it came from
  is removed, since it is now a skill instead of a note.

Either page's create form lets you pick the scope (repository, or "no
repository" for global) at creation time; scope is fixed once a memory is
saved — there is no later move between buckets, only delete-and-recreate.

## Durability

Memories are meant to survive well past the run that wrote them: they live in
the same database as everything else, are not tied to a session or a chat
transcript, and are read back by future runs through recall and
`search_memory` for as long as they exist. The only automatic removal is
eviction once an agent's memory count exceeds the configured cap (200 by
default, `evolution.memory_max_count`) — the oldest memories go first, and
the cap is per (agent, repository) bucket, so a busy repository cannot evict
what the same agent learned working somewhere else. Team memories are exempt
from eviction entirely, since they are meant to be a durable shared record
rather than a rolling window.

Self-reflection (see [Self-evolution and KPIs](self-evolution.md)) can
propose new memories too, but only **global** ones — a reflection reasons
over runs across every repository at once, so it has no single project to
anchor a project-scoped memory to.

## Promoting a memory to a skill by hand

Besides the automatic classifier on `save_memory`, the Team Memory page lets
you drive the same idea deliberately: it can scan the team's memories and
propose a plan — for each candidate memory, the skill name, description,
category and content it would become, and which agents would receive it.
Nothing is written while you are looking at the plan; only once you select
which candidates to apply does each one turn into an actual skill on those
agents, with the source memory removed. This is the deliberate, reviewable
counterpart to the classifier that runs inline on every `save_memory` call —
useful for cleaning up a backlog of team memories that accumulated before you
started using it, rather than one at a time as they are written.

## Where a run's repository comes from

Whether a memory save or search lands as project-scoped depends on whether
the run has a repository in context at all, and that comes from different
places depending on how the run started: a board run gets it from the task's
own repository, and a chat gets it from the project the conversation is
attached to. A chat with no project attached, or a background job with no
task, has no repository in context — so it can only read and write global
memory, and `scope=project` on `save_memory` is refused there rather than
guessing.

## A worked example

Say your backend agent keeps rediscovering that this repository's tests need
a specific environment variable set before `go test` will pass. Once it
notices the pattern, it can save `"this repository's tests require
TEST_DATABASE_URL to be set; without it, package tests fail with a
connection refused error"` with `scope=project` (since it is true only in
this repository) and `shared=false` or `shared=true` depending on whether
every agent working this repository should know it, not just this one. The
next run on this repository — this agent's or, if shared, any agent's — gets
that fact back through recall without anyone having to search for it, and
without it ever showing up in an unrelated repository's runs.

## Search is semantic when it can be

`search_memory` (and the duplicate check behind every save) ranks results by
meaning when an embedding model is configured, and falls back to
recency-ordering when it is not — so memory still works on an installation
that has not set up embeddings, just without the semantic ranking. See
[Skills and rules](skills-and-rules.md) for the same trade-off applied to
skill search.
