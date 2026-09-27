---
key: briefs.deploy.local_setup_task
version: 1
inputs: [ScriptPath, Kind]
---
Write {{.ScriptPath}}: a COMPLETE, executable bootstrap script that gets this {{.Kind}} running on a developer's own machine. A script, not a markdown guide — do not write one.

Definition of done — running it once on a fresh machine leaves the project running, with no other step:
- installs every dependency and toolchain the project needs (checks first, installs only what is missing)
- prepares env/config: creates the .env (or equivalent) from the example with local defaults, runs the migrations and seeds a first run needs
- starts every service the project needs to actually work — the app plus its database, cache, queue or emulator — not just the app process
- idempotent: a second run is safe and duplicates nothing
- executable (`chmod +x`), `#!/usr/bin/env bash`, `set -euo pipefail`
- a short usage header comment at the top: what it does, how to run it, and the port/URL it comes up on
- accepts an optional port argument (`{{.ScriptPath}} [port]`) overriding the default, so it can be rerun when the default port is taken
- matches what the repo actually needs today (its real package manager, build tool, ports) — not a generic template
{{if eq .Kind "mobile"}}- covers both iOS (simulator) and Android (emulator) if the repo ships both platforms
{{else if eq .Kind "worker"}}- starts any queue/broker the worker needs locally (or configures a fake/in-memory mode when there is none to start)
{{end}}