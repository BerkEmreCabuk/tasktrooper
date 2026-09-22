# Coding standards — server

Rules every agent writing Go in `server/` must follow. They live here so the
rules are stated next to the code they govern; [CLAUDE.md](../CLAUDE.md) is the
short pointer and the source of truth for local-mode invariants.

## Comments

Follow this rule, verbatim:

> Do not add code comments unless truly necessary — a non-obvious invariant, a
> workaround, or a WHY that isn't clear from the code itself. Never explain WHAT
> the code does; well-named identifiers already do that.

Concretely:

- **Delete** comments that restate the code below them ("iterate over the
  results", "return the sum"), narrate a function step by step, or repeat the
  identifier's name as its only content.
- **Delete** comment blobs that duplicate what another comment already said in
  the same file, and boilerplate doc comments on trivial getters/setters.
- **Keep** doc comments on exported identifiers when they carry non-obvious
  contract detail (parameter semantics, error behavior, invariants). A bare
  `// Foo returns the foo.` on a trivial method is not that, and should go.
- **Keep** comments that record a WHY: a non-obvious invariant, a workaround, a
  migration constraint, or a trap the code dodges that reciting the code would
  not reveal.
- **Keep** pragmas and directives: `//go:generate`, `//nolint`, `//nolint:...`,
  license headers.
- In tests, the test name and the assertions are the documentation. A comment is
  allowed only for a non-obvious fixture choice or an ordering requirement.

## Hexagonal boundaries

- `domain` and `application` never import `adapter`.
- New external integrations go behind a `port` interface; the adapter lives in
  `internal/adapter`.

## Interfaces and mocks

- Port interfaces are declared in `internal/port/*.go`, one concept per file.
- Mocks for interfaces are **generated with mockery**, never written by hand.
  See [testing-standards.md](testing-standards.md) for the `go generate` wiring.
- Hand-written `fake*`/`stub*` types in tests are allowed only for stateful
  in-memory stores whose behavior matters across many calls; a mock of a
  one-method seam is mockery-generated.

## Naming and layout

- Follow Go conventions: types are CamelCase, unexported helpers lowerCamelCase,
  acronyms keep their case.
- Error values live in the package that produces them; sentinel errors are not
  smuggled across the `domain` boundary unless the domain owns the fact.
- Tables in tests are `[]struct{ name string; ... }`, iterated with `for _, tt :=
  range tests { t.Run(tt.name, ...) }` (or `s.Run` inside a suite).