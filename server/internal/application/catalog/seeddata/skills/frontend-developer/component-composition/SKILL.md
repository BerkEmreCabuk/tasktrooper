---
name: component-composition
category: frontend
description: Use when building or changing any web UI - compose from the shared atomic component library (atom/molecule/organism/template/page), reuse before building, and keep components presentation-only
tech_stack: React
---

# Component Composition (Atomic Design)

## Overview

All web UI follows Atomic Design and a shared component library. The quality bar is reuse and correct placement: a new screen is assembled from existing atoms and molecules, and anything genuinely new is added at the right level so the next screen reuses it too.

**Core principle:** Reuse before you build. Before writing any button, card, badge, input, dialog, header, empty-state, or list-row, check the shared library — hand-rolling one that exists is a review-blocking defect.

## The Levels

| Level | What | Examples |
|-------|------|----------|
| **Atom** | Smallest reusable UI, no app logic | Button, Input, Badge, Avatar, Icon |
| **Molecule** | A few atoms as a unit | FormField (label+input+error), SearchBar, ListRow |
| **Organism** | Section from molecules/atoms | PageHeader, FormDialog, DataTable, EmptyState |
| **Template** | Page skeleton/layout, no data | TwoColumnLayout, DetailShell |
| **Page** | Route wiring organisms to data/state | ProjectBoardPage |

## Rules

- **Reuse the established shared components first** (PageHeader, FormDialog, EmptyState, ConfirmDialog, list rows, etc.). If one almost fits, extend it backward-compatibly — never fork a near-duplicate.
- **Place new components at the correct level.** A button is an atom; a card of atoms is a molecule; a page section is an organism. A "component" that fetches data is not a low-level component — split presentation from data.
- **Presentation-only below the page.** Atoms/molecules/organisms take props + callbacks; data fetching and state wiring live in the page (or a container organism). This keeps them reusable and testable.
- **Composition over prop explosions.** Prefer `children`/slots over a pile of `isX` booleans — three `isX` props usually mean two components.
- **Split large pages.** A component you must scroll to read is doing too much — extract focused children.
- **Changing a shared component:** grep every caller first; keep it backward-compatible or update all callers in the same task. `npm run build` must stay green.

## Worked Example

Task: "add a project settings panel with a name field and a danger-zone delete."

Wrong: one 300-line component with hand-built inputs, a custom button, and an inline confirm modal.

Right:
1. Grep the library → `FormField` (molecule), `Button` (atom), `ConfirmDialog` (organism) all exist.
2. New: `DangerZone` organism (composes a `Button variant="destructive"` + `ConfirmDialog`) — reusable on other settings screens.
3. `ProjectSettingsPage` wires the data (load name, save, delete) and composes `PageHeader` + `FormField` + `DangerZone`.
4. Only `DangerZone` is new code; everything else is reuse. It's added at the organism level so the next settings screen reuses it.

## Common Mistakes

- Rebuilding an existing atom inline instead of importing it.
- A low-level component that calls an API → move data to the page.
- Boolean-prop explosions instead of composition/slots.
- Breaking a shared component's props without updating callers.
- Dumping a new component at the wrong level (a page-specific blob where an atom belongs).

## Red Flags

- Two screens render visually identical controls built differently.
- An atom/molecule imports a data hook or API client.
- A component file you scroll to read.
