---
title: MCP servers
description: Connect your own MCP servers to your agents, and how that differs from the board tools a Claude Code session already gets.
---

**Settings → MCP Servers** connects external MCP servers to your agents —
your own tools, reached over stdio or HTTP, alongside the built-in terminal,
file, web and board tools every agent already has.

This is the opposite direction from the MCP endpoint a Claude Code session
calls back on for the board's own tools. See [the difference](#the-other-direction-tasktroopers-own-mcp-endpoint)
below, and [Agent CLIs and API providers](runtimes.md#how-a-run-is-started)
for how that endpoint works.

## Adding a server

Open **Settings → MCP Servers** and choose **Add Server**. You either pick a
ready-made template or start from a custom configuration; every field is
editable before you save either way.

Every server needs:

| Field | Meaning |
|---|---|
| ID | A short name with no spaces or slashes. This becomes the namespace every one of the server's tools is served under, and it cannot be changed later without recreating the server. |
| Transport | `stdio` (a local process) or `http` (an existing MCP endpoint) |
| Enabled | Whether the server connects at all. A disabled server is kept configured but not dialled. |

**stdio** servers need a `command` and its `args` — the same shape as a
`.mcp.json` entry, run as a local child process (`npx -y
@modelcontextprotocol/server-github`, for example). **http** servers need a
`url` and, optionally, request `headers`.

An `allowed_tools` list on the server narrows which of its tools are actually
registered; leave it empty to take every tool the server advertises.

An HTTP server's URL is checked against the same outbound guard every other
agent-reachable URL goes through (loopback, link-local and private-network
addresses are refused) before the server is ever saved — see [Data directory
and security](data-and-security.md) for what that guard covers.

## Templates

Seven templates ship with the app, each pre-filling the transport, command or
URL and naming which fields are secrets:

| Template | Transport | What it needs |
|---|---|---|
| Filesystem | stdio | A root directory to expose |
| Git | stdio | A repository path |
| GitHub | stdio | `GITHUB_PERSONAL_ACCESS_TOKEN` |
| PostgreSQL | stdio | A connection URL |
| Slack | stdio | `SLACK_BOT_TOKEN`, `SLACK_TEAM_ID` |
| Hugging Face | http | A bearer token, sent as the `Authorization` header |
| Browser | stdio | Nothing — enabled by default |

Picking a template fills the form with its defaults; you still choose the
server's ID, fill in whatever it asks for (a token, a path, a connection
string), and can edit anything else — command, args, extra environment
variables — before saving. A template already added to your list of servers
is not offered again. **Custom** starts from a blank stdio server instead of
a template.

## How the tools are named

Every tool a connected server advertises is registered as
**`mcp_<server_id>_<tool_name>`** — the server's ID, then the tool's own
name, exactly as the MCP server defined it. An agent whose tool policy allows
the server (or the specific tool) sees it under that name and calls it like
any other tool; its parameters and description come straight from the
server's own tool definition, unchanged.

This is a flat namespace on purpose: a GitHub server with ID `github` and a
tool called `create_issue` is served as `mcp_github_create_issue`, and
nothing about the name changes based on which agent is calling it or which
column the run is in. [Tool policies](tool-policies.md) control access the
same way they control every built-in tool — by name or by server ID.

## Secrets

A field a template marks as secret — a token, a key, a password — is
encrypted at rest and never sent back to the browser in the clear: the
server list shows a masked placeholder for it instead of the stored value,
and saving the form again without touching that field leaves the stored
secret alone. A custom server infers which of its own `env`/`headers` values
look like secrets the same way, rather than requiring every field to be
hand-flagged.

Non-secret fields — a repository path, a team ID, a Postgres host with no
credentials in it — are stored as plain configuration next to the server, not
encrypted, since masking them would only make the form harder to read.

Deleting a server removes its stored secrets along with it; there's nothing
left over to clean up by hand.

## Connection status

Each row in the MCP Servers table shows:

| Column | What it means |
|---|---|
| Transport | `stdio` or `http` |
| Status | **Connected**, **Inactive** (disabled), or **Error** |
| Tools | How many tools the server is currently serving, expandable to the list of names |
| Enabled | The toggle that connects or disconnects the server without deleting it |

An enabled server that failed to connect — a bad command, an unreachable
URL, a stdio process that exited — shows **Error** with the failure reason;
toggling it off and back on, or editing and saving it, retries the
connection. The table refreshes in the background every few seconds while
the page is open.

## The other direction: TaskTrooper's own MCP endpoint

MCP servers you connect here are tools your agents call *out* to. TaskTrooper
also runs the reverse: a `/mcp` endpoint the server mounts on its own port,
which is how a Claude Code session reaches the board itself — moving cards,
ticking acceptance criteria, reading a task's pull request, and so on. That
endpoint:

- serves tools named `mcp__tasktrooper__<name>`, a different namespace from
  `mcp_<server_id>_<tool_name>`;
- is bound to `127.0.0.1` and guarded by a token minted for that one run (or,
  in chat, that one turn) and revoked when it ends — nothing you configure
  here;
- is the *only* MCP configuration a board or chat session ever loads. The
  session is started with `--strict-mcp-config`, which means a repository's
  own checked-in `.mcp.json` and your personal Claude Code settings under
  `~/.claude` are never read into a TaskTrooper run — servers connected here
  are what stand in for them.

In other words: the servers on this page are what a TaskTrooper agent can
call; the `/mcp` endpoint is what a TaskTrooper agent already answers to
Claude Code. See [Agent CLIs and API providers](runtimes.md#how-a-run-is-started)
for the run-side detail on that endpoint.

## Where this is used

A connected, enabled server's tools become available to every agent whose
tool policy admits them — board runs and chats alike, on any provider. There
is no per-repository or per-task MCP configuration: a server you add here is
available everywhere its tool policy allows it, the same way a built-in tool
is.
