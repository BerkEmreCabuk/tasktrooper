# Security policy

## Supported versions

Only the latest release gets security fixes. The desktop app updates itself, so
the fix ships as the next patch release.

## Reporting a vulnerability

Please do not open a public issue. Report it privately through
[GitHub's private vulnerability reporting](https://github.com/makifbaysal/tasktrooper/security/advisories/new)
(the repository's **Security** tab → **Report a vulnerability**).

Include what you found, the version, the OS, and the steps or a proof of concept
that reproduce it. You should get a first answer within a week. Once a fix is
released, the advisory is published with credit to you unless you ask otherwise.

## Scope

TaskTrooper runs on one machine for one user: the backend binds `127.0.0.1` only
and every request needs the bearer token the desktop app generates at start.
Reports that matter most:

- reaching the backend without that token, or from another machine
- another local user or process reading the token, stored provider credentials
  or the embedded Postgres data
- a web page, repository, prompt or agent output that gets code executed outside
  what the user asked the agent to do
- secrets leaking into logs, argv or anything sent off the machine

Out of scope: attacks that already need the same user's shell on the machine,
and the behavior of the third-party agent CLIs and model providers themselves.
