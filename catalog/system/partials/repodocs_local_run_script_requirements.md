---
key: partial.repodocs_local_run_script_requirements
version: 1
inputs: [FullPath]
---
This one is NOT a markdown guide: {{.FullPath}} must be a COMPLETE, executable local bootstrap script.
Running it on a fresh machine must leave the project running, with no other step:
- install every dependency and toolchain the project needs (check first, install only what is missing)
- prepare env/config: create the .env (or equivalent) from the example, fill in the local defaults, run whatever migrations and seeds a first run needs
- start every service the project needs to actually work — the app plus its database, cache, queue or emulator — not just the app process
- idempotent: running it a second time must be safe and must not duplicate anything
- executable (`chmod +x`), with a `#!/usr/bin/env bash` shebang and `set -euo pipefail`
- a short usage header comment at the top: what it does, how to run it, and the port/URL it comes up on
- accepts an optional port argument (`{{.FullPath}} [port]`) overriding the default, so it can be rerun when the default port is already in use

