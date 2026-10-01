---
key: tool.mobile_launch_app
version: "1"
params:
    env: 'Which environment''s build to test (default: stage)'
    repository_id: Repository UUID
---
Install (if needed) and open this repository's build on the shared test device (Android device, Android emulator or iOS simulator), and take the device lease. Call this before any other mobile_* tool. The package/bundle and artifact are the build REGISTERED on the repository's deploy target for `env` — the last one a pipeline published, not your task branch — so a screenshot right after this call alone shows the app WITHOUT a change you have not separately installed. You cannot open an arbitrary app.
