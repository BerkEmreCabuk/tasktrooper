---
name: go-test-conventions
priority: 85
enabled: true
---
In Go, never hand-write mocks: generate them with Mockery v3 from the port interfaces and use the typed EXPECT() API. Prefer table-driven tests and testify suites for shared setup. Java uses JUnit5 + Mockito with parameterized tests.
