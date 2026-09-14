---
name: flutter-testing
category: testing
description: Use when testing Flutter code - unit tests for logic, widget tests for UI behavior, golden tests for appearance, test-first
tech_stack: Flutter
---

# Flutter Testing

## Overview

Test-first in Flutter (see tdd-workflow). Three layers, each for a different question: does the logic work, does the widget behave, does it look right.

**Core principle:** Most tests are fast unit + widget tests. Golden tests guard appearance; integration tests guard whole flows — use them sparingly.

## Test layers

| Layer | Question | Tool |
|-------|----------|------|
| Unit | Does the controller/logic compute correctly? | `flutter_test`, mock the repository |
| Widget | Does the widget render state and respond to taps? | `testWidgets` + `pumpWidget` + finders |
| Golden | Does it match the approved pixels? | `matchesGoldenFile` |
| Integration | Does the whole flow work on a device? | `integration_test` |

## Widget test example

```dart
testWidgets('shows error view and retries', (tester) async {
  final controller = FakeTaskController()..emitError('boom');
  await tester.pumpWidget(wrap(TaskListScreen(controller: controller)));
  await tester.pump();

  expect(find.text('boom'), findsOneWidget);
  await tester.tap(find.byType(RetryButton));
  await tester.pump();
  expect(controller.loadCalled, isTrue);   // intent reached the controller
});
```

## Rules

- **Unit-test controllers/logic without pumping a widget** — that's the payoff of keeping logic out of widgets (see flutter-state-management).
- **Widget tests assert observable behavior** (text visible, tap triggers intent), not internal structure.
- **Mock the repository/ports**, not the widget under test. Prefer a hand-written fake or `mocktail` per the app's convention.
- **Golden tests:** commit the golden, review changes deliberately; regenerate only when the change is intended.
- Cover loading, data, empty, and error states — the branches most likely to ship broken.
- `pump` vs `pumpAndSettle`: use `pump` for controlled frames, `pumpAndSettle` to drain animations — never rely on real timers.

## Common Mistakes

- `pumpWidget` for logic that could be a plain unit test (slow, indirect).
- Asserting widget-tree internals instead of visible behavior.
- No error/empty-state test.
- Golden files regenerated blindly, hiding a visual regression.

## Red Flags

- The test passes before the production code exists (tests a fake).
- Every test boots the full app.
- Only the happy path is covered.
