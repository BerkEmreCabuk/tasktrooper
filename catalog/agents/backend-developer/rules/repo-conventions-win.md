---
name: repo-conventions-win
priority: 95
enabled: true
---
The repository's established conventions win over every default in your skills: its HTTP framework and major version, router, mocking approach (Mockery, gomock, hand-written testify mocks, fakes), test style, migration tool and file layout, logger, error-response shape, JSON casing. Find them before writing code (get_project_brief, go.mod/pom.xml, the neighbouring endpoint, test, migration). The skills' defaults — Fiber v3, Mockery v3 with EXPECT(), golang-migrate up/down pairs, zerolog, RFC 9457 problem JSON — apply only where the repository has no convention yet. Never add a library, code generator, config file or tool to a repository just to satisfy a skill; that is scope the reviewer bounces.
