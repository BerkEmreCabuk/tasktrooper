---
key: tool.update_deploy_target
version: "1"
params:
    app_url: Where this environment's installable mobile build currently lives (the APK the pipeline published). QA installs from it onto the test device. Omit to leave it as it is. The package name itself is set by a human and cannot be changed here.
    base_url: Where the environment answers, from the deploy output (e.g. https://api-stage.example.com). Omit to leave it as it is.
    env: stage | preprod | prod
    health_url: The URL the production monitor should poll (e.g. https://api-stage.example.com/health). Omit to leave it as it is.
    logs_url: An endpoint THIS APPLICATION serves that returns its recent logs (e.g. https://api-stage.example.com/internal/logs). get_deploy_logs reads it after a deploy, which is how a deploy that came up but is logging errors gets noticed — health_url only ever answers "it is up". Record it only if the app actually exposes such a route; do not invent one, and do not point it at a cloud provider's log console. Omit to leave it as it is.
    repository_id: Repository UUID (the repository_id in your task snapshot). Omit to write to the repository of the task this run is working on. Anything that is not a UUID is refused — this tool writes, so it does not guess.
---
Record where an environment actually answers (base URL / health URL / logs URL) after a deploy — keep deploy targets truthful. It writes nothing else: provider, template and variables stay as the human configured them.
