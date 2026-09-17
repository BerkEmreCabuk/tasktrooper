---
title: Tool policies
description: What an agent may call, how the seeded roles differ, and how the column an agent is working in narrows that further.
---

A **tool policy** is an allow-list: the tool names (and MCP server ids) an
agent's sessions are permitted to call. It lives on the agent's Settings tab
under **Tool policy**, alongside the model and provider. An agent with an
empty allow-list is unrestricted — every registered tool is available to it —
so scoping an agent down is something you do deliberately, not something
that happens by leaving a field blank.

Two independent things narrow what actually runs in a given session: the
**agent's** policy (what this agent, as a role, is allowed to do at all) and
the **column** a task sits in (what makes sense for the job that column's
run is there to do). The agent's policy is the wider of the two; the column
narrows it further for that one run.

## What the seeded roles get

The three developer roles (`backend-developer`, `frontend-developer`,
`mobile-developer`) get essentially everything a coding agent needs:

- The terminal (`run_terminal`) and every file writer (`write_file`,
  `edit_file`, `edit_lines`, `delete_file`, `move_file`, `download_file`) — the
  shell and the writers always travel together, since anything a shell can do
  with `sed -i` a file tool can do explicitly.
- Web search and page fetch.
- The read-only code tools (`codebase_search`, `grep_code`, `get_repo_tree`,
  `get_symbol_skeleton`, `expand_symbol_context`, `read_file`).
- The browser tools, so a developer can open the page it just changed on
  `127.0.0.1` before handing the task on, not just get a green build.
- Board read tools, claiming a task, memory, skills, and reading/replying to
  the task's pull request and committing to it.
- The mobile developer additionally gets the device tools
  (`mobile_launch_app`, `mobile_tap`, `mobile_screenshot`, and so on) — the
  other two developer roles do not, since the device is one shared phone and
  only mobile work has a use for it.

**`product-manager`** gets no shell and no file writers — it works the board,
not the code. It gets the read-only code tools (to verify a real file or
endpoint name before writing a technical description, never to change one),
the browser and mobile tools (for walking a build in PM UAT), board create and
delete, workspace management (creating/renaming projects), and its own
criterion verdicts (`review_criterion`).

**`system-architect`** gets the shell, the read-only code tools, board create
and claim, and read/reply on pull requests — but not the file writers or
`commit_task_changes`. It is the code reviewer: it reads the diff it is
judging and can comment on it, but cannot push a fix into the branch under
review.

**`qa-agent`** gets the shell, the browser and mobile tools, and a
deliberately narrow slice of the code tools — see below — plus the one
merge tool (`merge_task_pull_request`) and the release-watch tools
(deploy status, deploy logs, rollback), both of which only actually work in
the `done`/`released` columns (see [Column narrowing](#how-a-column-narrows-a-policy) below). QA is
the only role with the merge tool: the developer must not merge its own
branch, and the reviewer and PM judge the change rather than land it.

## The QA black-box rule

QA does **not** get the general code-reading tools
(`codebase_search`, `get_symbol_skeleton`, `expand_symbol_context`). It gets a
narrower lookup set instead:

- `get_repo_tree`
- `grep_code`
- `read_file`

This is deliberate: QA's evidence is a running product, not a read of the
diff. Early on, QA held the full code-reading set and used it exactly as
those tools invite — a round on a UI task opened with a tree listing and a
handful of `read_file` calls, and the "test" that came out was a reading of
the source, not a test of the behavior. The tools that exist to help someone
*understand* code are the reviewer's tools; QA's job is to exercise the
build and report what it observed, so its lookup set only goes as far as
finding the start command and the route to open.

## How a column narrows a policy

Two functions apply after the agent's own policy has already decided what it
may do at all:

**`RestrictToolsForVerdictColumn`** strips the workspace writers, the commit
tool, and (outside the one column each survives in) the merge and rollback
tools from a run in a column whose job is to *judge*, not to *change*: Code
Review, Analysis Review, Ready for QA, In QA, PM UAT, Human UAT, and Done. The
terminal stays — QA still has to boot and run the product — but a reviewer or
QA run that finds a bug cannot go fix it itself; the finding has to go back
to the developer instead. The one write these columns allow is the merge,
and only in `done`, which is the column the whole sequence (merge, watch the
deploy, roll back if it fails) actually happens in.

**`RestrictToolsForTaskType`** strips the workspace writers from any run on
an `analiz` (analysis) task, regardless of column. An analysis task's
deliverable is a spec and a plan document attached to the task, never a code
change — there is nothing in that workflow for a file writer to do, and
without this restriction an architect dispatched on an analysis task has, in
practice, gone ahead and edited files that then died with the run's
workspace, having never been committed or reviewed.

**`RestrictToPlannedTools`** applies only inside chat orchestration, when a
request is broken into subtasks. A subtask's declared tool names decide
**only** the board-write tools (`create_board_task`, `move_board_task`,
`update_board_task`) — that declaration exists purely to stop two concurrent
subtasks from writing the same board record twice. It is not a capability
budget: every other tool the agent is configured for (code reading, the
shell, `load_skill`) stays available on every subtask regardless of what that
subtask declared, because narrowing them by declaration once left a subtask
holding only board tools unable to answer even "where are the files" and
asking a human instead.

The columns `RestrictToolsForVerdictColumn` treats as judging, not changing:

| Column | Writers/commit removed | Merge/rollback removed |
|---|---|---|
| Code Review | Yes | Yes |
| Analysis Review | Yes | Yes |
| Ready for QA | Yes | Yes |
| In QA | Yes | Yes |
| PM UAT | Yes | Yes |
| Human UAT | Yes | Yes |
| Done | Yes | No — this is where the merge and the deploy watch/rollback happen |

## Editing a policy

Open the agent's **Settings** tab; the tool policy editor sits below the
system prompt. It lets you add or remove individual tools and MCP servers
from the allow-list. Two things to know before narrowing one:

- **An empty list means unrestricted**, not "nothing" — to actually restrict
  an agent you list what it may use.
- **Tool policy is set only when an agent is created**, and never
  reconciled afterward (see [Role agents](role-agents.md#seeding-and-what-survives-an-upgrade)).
  Any change you make here on a seeded agent is permanent across restarts and
  upgrades; the one exception is a handful of paired grants (a tool that
  completes a capability you already hold) that reach existing installs
  automatically without touching anything you deliberately left out.

## The terminal and outbound URL guards

Two settings-level guards apply underneath every agent's policy, regardless
of what tools it is allowed:

- **`tools.terminal`** gates `run_terminal` itself: it must be explicitly
  enabled, and its `sandbox.mode` (`allowlist`, `blocklist`, or `off`) can
  restrict which commands run at all, independent of which agents are allowed
  to call the tool.
- **The outbound URL guard** sits under every tool that dials a
  model-chosen URL — `fetch_url`, the browser tools, search, MCP HTTP
  endpoints, health probes. It refuses loopback, link-local, private-network,
  and other internal address ranges by default, pins the request to the
  address it vetted so DNS cannot redirect it after the fact, and re-checks
  every redirect hop. The one exception is the browser tools, which default
  to *allowing* loopback — a QA run routinely needs to open the build it just
  started on `127.0.0.1`. Both defaults are controlled by the
  `ALLOW_LOOPBACK_TOOL_URLS` process environment variable, not a config file
  or admin setting, so nothing reachable through the API or the UI can widen
  it.
