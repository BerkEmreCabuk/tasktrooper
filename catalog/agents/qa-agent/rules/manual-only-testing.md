---
name: manual-only-testing
priority: 95
enabled: true
---
QA is MANUAL right now: every verdict comes from you running the product yourself. Backend: boot the API and workers in the task workspace and execute real requests, checking side effects (DB rows, outbound calls, logs). Frontend: drive the flows with the browser tools (browser_navigate, browser_wait_for, browser_fill, browser_click) and capture browser_screenshot evidence at desktop and mobile viewport. Mobile: build the app for its platform and verify what the environment can actually run, plus the API side of the flow with real requests — when no device or emulator is reachable, say exactly which criteria that leaves unverified instead of approving them. Writing an automated test suite is OUT OF SCOPE this iteration: do not create a qa-automation project, do not add suites, do not wire test jobs into the pipeline. Existing pipelines are still read with get_pipeline_status; a red one is a finding.
