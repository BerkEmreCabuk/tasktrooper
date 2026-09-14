---
name: accessibility-basics
category: frontend
description: Use on every UI change - semantic HTML, labels for controls, keyboard-navigable dialogs/menus, visible focus, and never color as the only signal
---

# Accessibility Basics

## Overview

Accessibility is not a pass at the end — it's built into each component. The recurring defects are `div` with `onClick` instead of a button, icon buttons with no label, and custom menus/dialogs that trap keyboard users.

**Core principle:** Semantic HTML and the accessible primitives do most of the work — reach for them first.

## Rules

- **Semantic HTML first:** `button` for actions, `a` for navigation, headings in order. A `div` with `onClick` is a defect (no keyboard, no role).
- **Every control is labeled:** every form control has an associated `<label>`; every icon-only button has an `aria-label`.
- **Dialogs and menus are keyboard-navigable:** focus moves in on open, is trapped while open, and returns to the trigger on close — the Radix primitives do this, so use them instead of hand-rolling.
- **Visible focus everywhere:** never remove an outline without a replacement focus style.
- **Color is never the only signal**; keep text contrast readable on both light and dark themes.

## Worked Example

```tsx
// ❌ not keyboard-accessible, no role, no label
<div className="icon-btn" onClick={onDelete}><TrashIcon /></div>

// ✅ real button, labeled, gets focus + Enter/Space for free
<button type="button" aria-label="Delete task" onClick={onDelete}>
  <TrashIcon aria-hidden />
</button>
```

The `button` is focusable and keyboard-activatable with no extra code; the `aria-label` gives screen readers the action; `aria-hidden` on the icon stops it being announced twice.

## Common Mistakes

- `div`/`span` with `onClick` for an action.
- Icon-only button with no `aria-label`.
- A hand-rolled dropdown/dialog that traps keyboard users → use the Radix primitive.
- Removing focus outlines with no replacement.
- Status shown by color alone.

## Red Flags

- A clickable element that isn't a `button`/`a`.
- An interactive widget you built from raw `div`s instead of a primitive.
- `outline: none` with nothing in its place.
