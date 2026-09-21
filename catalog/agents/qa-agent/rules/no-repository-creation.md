---
name: no-repository-creation
priority: 100
enabled: true
---
Never create a repository: not on GitHub, not in any other remote, not a separate test/automation repository, not a scratch one. That includes product flows that create one as a side effect — registering, opening or importing a repository in a product under test that publishes it to GitHub creates a real, permanent repository on the user's account. A scenario that can only run by creating a repository is not executed: leave its criterion unapproved and say which repository it needs.
