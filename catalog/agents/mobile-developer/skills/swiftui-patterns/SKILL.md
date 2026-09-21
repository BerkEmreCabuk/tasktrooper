---
name: swiftui-patterns
category: architecture
description: Use when building native iOS with SwiftUI - small composable views, observable state models, atomic components, and system styling over hardcoded values
tech_stack: Swift
---
# SwiftUI Patterns

## Overview

Native iOS (SwiftUI) is chosen when a task needs deep Apple-platform integration or the app is already native (see native-vs-flutter-decision). Same discipline as Flutter: small composed views, state out of the view, reusable atomic components.

**Core principle:** Views are a function of state. Data and business logic live in an observable model, not in the `View`.

## Rules

- **Small views, composed.** Extract subviews as their own `View` types (not `@ViewBuilder` funcs) so each has its own body and preview.
- **State ownership is explicit:** `@State` for local ephemeral UI only; app/business state in an `@Observable` model (or `ObservableObject`) injected via `@Environment`/init. Views send intents to the model; the model owns the logic and the networking.
- **Atomic components:** a shared component library (atoms → molecules → organisms) of reusable views. Reuse before building; grep before adding.
- **System styling over hardcoded values:** colors from the asset catalog / `Color` semantic roles, spacing/typography from a tokens file — not scattered magic numbers. Support Dynamic Type and dark mode.
- **`#Preview`** every reusable view in its key states (loading/data/error) — previews are your fast feedback loop.
- **Value types** (`struct`) for models and view state; reference types only where identity is needed.

## Worked Example

```swift
@Observable final class TaskListModel {          // owns state + logic, testable
    private(set) var state: LoadState<[Task]> = .loading
    private let repo: TaskRepository
    init(repo: TaskRepository) { self.repo = repo }
    func load() async {
        state = .loading
        do { state = .data(try await repo.list()) } catch { state = .error(error) }
    }
}

struct TaskListScreen: View {                     // renders state, sends intents, no logic
    @State private var model: TaskListModel
    var body: some View {
        switch model.state {
        case .loading: AppSpinner()
        case .error(let e): AppErrorView(message: e.localizedDescription) { Task { await model.load() } }
        case .data(let tasks): TaskList(tasks: tasks)
        }
    }
}
```

## Rules for testing

- Unit-test the `@Observable` model with a mocked repository (no UI).
- Use XCTest / Swift Testing for logic; snapshot or ViewInspector-style checks for views where the repo uses them. Test-first.

## Common Mistakes

- Networking or business rules inside `body` / `View`.
- `@ViewBuilder` helper funcs instead of real subviews.
- Hardcoded colors/sizes instead of asset-catalog + tokens.
- No previews, so every change needs a full build to see.

## Red Flags

- A `View` with a URLSession call in it.
- Magic `Color(red:...)` and paddings sprinkled across views.
- A 200-line `body`.
