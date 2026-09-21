---
name: flutter-atomic-components
category: architecture
description: Use when adding or reusing Flutter UI - organize widgets as atoms, molecules, and organisms in a shared library and never hand-roll a component that already exists
tech_stack: Flutter
---
# Flutter Atomic Components

## Overview

The same atomic-design discipline the web app follows applies to Flutter: build a shared component library organized by level and compose screens from it. This keeps the app visually consistent and makes screens small.

**Core principle:** Reuse before you build. Before writing any button/card/chip/input/list-row, search the component library — hand-rolling a duplicate is a review-blocking defect.

## The Levels

| Level | What | Flutter example |
|-------|------|-----------------|
| **Atom** | Smallest reusable UI, no app logic | `AppButton`, `AppTextField`, `StatusChip`, `Avatar` |
| **Molecule** | A few atoms forming a unit | `TaskCard` (title + chip + avatar), `SearchBar` |
| **Organism** | Section built from molecules/atoms | `TaskList`, `BoardColumn`, `AppBarWithActions` |
| **Screen** | Route wiring organisms to state | `TaskBoardScreen` |

Library lives under `lib/ui/atoms`, `lib/ui/molecules`, `lib/ui/organisms` (follow the app's existing layout if different).

## Rules

- **Before creating a component, grep the library.** If an atom exists, use it. If it almost fits, extend it backward-compatibly — don't fork a near-duplicate.
- **Place new components at the correct level.** A button is an atom; a card composed of atoms is a molecule. A "component" that fetches data is not a component — split the presentation out.
- **Atoms take data + callbacks, never talk to services.** `AppButton({required this.label, required this.onPressed})`. State/data flows from the screen down.
- **Style through the theme** (see flutter-widget-architecture) so every instance of an atom looks identical.
- **Changing a shared atom:** grep every call site first; keep the change backward-compatible or update all callers in the same task. The build must stay green.

## Worked Example

Task: "add a priority badge to task cards." Wrong: add a `Container` with a colored label inline in `TaskCard`. Right:
1. Grep atoms → no `PriorityBadge`, but there is a `StatusChip` atom. Priority is distinct enough → add a `PriorityBadge` atom next to it, themed the same way.
2. Compose it into the `TaskCard` molecule.
3. Both the badge atom and the card get a widget test.

Now every screen showing a task card gets the badge for free, consistently.

## Common Mistakes

- Inline `Container`/`Text` styling that reinvents an existing atom.
- A component with a network/service call inside → not a component.
- New component dumped at the wrong level (an organism where an atom belongs).
- Breaking a shared atom's constructor without updating callers.

## Red Flags

- Two screens render visually identical buttons built differently.
- A "widget" imports a repository or http client.
- Copy-pasted styled `Container`s across screens.
