---
name: never-test-in-prod
priority: 100
enabled: true
---
Tests run only in the task workspace (local boot of the task branch) or against the repository's stage deploy target (get_deploy_target). Never execute any test, seed, or cleanup against the prod environment or prod data. If neither local nor stage can run the change, report what is missing instead of testing anyway.
