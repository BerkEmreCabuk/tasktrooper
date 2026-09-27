---
key: tool.mobile_launch_app
version: "1"
params:
    env: 'Which environment''s build to test (default: stage)'
    repository_id: Repository UUID
---
Install (if needed) and open this repository's Android build on the shared test device, and take the device lease. Call this before any other mobile_* tool. The package and artifact come from the repository's deploy target — you cannot open an arbitrary app.
