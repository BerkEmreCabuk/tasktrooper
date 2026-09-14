---
name: android-compose-patterns
category: architecture
description: Use when building native Android with Jetpack Compose - stateless composables, state hoisting, ViewModel-owned state, atomic components, and Material theming over hardcoded values
tech_stack: Kotlin
---

# Jetpack Compose Patterns

## Overview

Native Android (Jetpack Compose) is chosen when a task needs deep Android integration or the app is already native (see native-vs-flutter-decision). Same discipline as Flutter/SwiftUI: small stateless composables, state hoisted out, reusable atomic components.

**Core principle:** Composables are stateless functions of state. State is hoisted to a `ViewModel`; composables emit events upward.

## Rules

- **Stateless composables + state hoisting.** A composable takes its state as parameters and exposes callbacks (`onXxx`) — it does not own app state. The screen-level composable connects a `ViewModel` to the stateless UI.
- **`ViewModel` owns state and logic**, exposed as `StateFlow`/`UiState`; the composable collects it with `collectAsStateWithLifecycle()`. Networking/business rules live in the ViewModel + repository, never in the composable.
- **Model UI state as a sealed hierarchy:** `Loading | Data | Empty | Error` — `when` over it renders every branch. No happy-path-only screens.
- **Atomic components:** shared library of reusable composables (atoms → molecules → organisms). Reuse before building; grep before adding.
- **Material theme, not hardcoded values:** `MaterialTheme.colorScheme` / `typography` / a spacing tokens object — never scattered `Color(0xFF...)` and magic `dp`. Support dark theme + dynamic color.
- **`remember`/`derivedStateOf`** to avoid recomputation; **hoist** `remember`ed state only when it's genuinely local (scroll, text field).
- **`@Preview`** reusable composables in their key states.

## Worked Example

```kotlin
// ViewModel owns state + logic (unit-testable, no Compose)
class TaskListViewModel(private val repo: TaskRepository) : ViewModel() {
    private val _state = MutableStateFlow<UiState<List<Task>>>(UiState.Loading)
    val state: StateFlow<UiState<List<Task>>> = _state
    fun load() = viewModelScope.launch {
        _state.value = UiState.Loading
        _state.value = runCatching { repo.list() }.fold(UiState::Data, UiState::Error)
    }
}

// stateless composable renders state, emits events
@Composable
fun TaskListScreen(state: UiState<List<Task>>, onRetry: () -> Unit) = when (state) {
    is UiState.Loading -> AppSpinner()
    is UiState.Error   -> AppErrorView(message = state.cause.message, onRetry = onRetry)
    is UiState.Data    -> TaskList(tasks = state.value)
    is UiState.Empty   -> EmptyView()
}
```

## Testing

- Unit-test the `ViewModel` with a fake/mockk repository and a test dispatcher — no Compose.
- Compose UI tests (`createComposeRule`) assert visible behavior and clicks. Test-first.

## Common Mistakes

- A composable holding app state or calling the repository.
- Business logic in the composable instead of the ViewModel.
- Hardcoded colors/dp instead of `MaterialTheme` + tokens.
- Collecting flows without `collectAsStateWithLifecycle` (leaks/waste).
- Only the data branch rendered.

## Red Flags

- A `@Composable` that imports a repository or Retrofit service.
- `Color(0xFF...)`/magic `dp` outside the theme.
- No error/empty branch in the `when`.
