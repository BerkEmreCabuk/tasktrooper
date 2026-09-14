---
name: vite-tailwind-radix
category: frontend
description: Use when styling any web UI - style with Tailwind utility classes and design tokens, avoid custom CSS, and build on the existing Radix/shadcn primitives instead of hand-rolling
tech_stack: React
---

# Tailwind, Radix, No Custom CSS

## Overview

Styling is Tailwind-first with a shared token system, on top of Radix/shadcn primitives. Custom CSS and hand-rolled interactive widgets are what this skill exists to prevent — they fragment the design and re-introduce accessibility bugs the primitives already solved.

**Core principle:** Avoid custom CSS. Style with Tailwind utilities and the design tokens; reach for the existing primitive before building an interactive widget.

## Rules

- **Tailwind utilities, not custom CSS.** No new `.css` files, no `<style>` blocks, no bespoke class names. Compose utilities; use `cn()` for conditional variants. An inline `style={{}}` object is allowed ONLY for a genuinely dynamic, computed value (a measured width, a transform), never for static styling.
- **Design tokens, not magic values.** Use the established spacing/color/radius/typography scale (the shadcn-style token classes) that neighboring screens use — not arbitrary `w-[327px]` or `text-[#3a3a3a]`. Consistency comes from reusing the scale.
- **Build on the primitives.** Dialog, Dropdown, Tooltip, Popover, Tabs, etc. come from the Radix primitives wrapped in `components/ui` — never hand-roll one that exists (you'll lose focus trapping, keyboard nav, and ARIA).
- **Match neighbors.** Before styling a new screen, look at an adjacent one and reuse its patterns and token classes.
- **`npm run build` must stay green** — a TypeScript error or failed build is a broken task, not a warning.

## When an inline style / arbitrary value IS ok

| Situation | Verdict |
|-----------|---------|
| Static padding/color/size | Tailwind token class |
| Conditional variant | `cn()` with utility classes |
| Value computed at runtime (measured px, dynamic transform) | inline `style={{}}` — the only valid case |
| A one-off pixel that "isn't in the scale" | No — use the nearest scale token; extend the scale via config if truly needed |

## Worked Example

```tsx
// ❌ custom CSS + magic values + hand-rolled dropdown
<div className="my-custom-menu" style={{ padding: "13px", background: "#f5f5f5" }}>
  {/* hand-built menu: no keyboard nav, no ARIA */}
</div>

// ✅ tokens + primitive
<DropdownMenu>                          {/* Radix primitive from components/ui */}
  <DropdownMenuTrigger asChild><Button variant="ghost">Actions</Button></DropdownMenuTrigger>
  <DropdownMenuContent className="p-3 bg-muted">   {/* token classes */}
    <DropdownMenuItem onSelect={onExport}>Export</DropdownMenuItem>
  </DropdownMenuContent>
</DropdownMenu>
```

The primitive brings focus management, escape-to-close, and ARIA for free; the token classes keep it visually identical to every other menu.

## Common Mistakes

- A new `.css` file or `<style>` block instead of utilities.
- Arbitrary values (`w-[327px]`, `text-[#3a3a3a]`) instead of scale tokens.
- Hand-rolling a dialog/dropdown/tooltip that exists in `components/ui`.
- `style={{}}` for static styling that a class would cover.

## Red Flags

- Any new custom CSS class or stylesheet in the diff.
- An interactive widget built from raw `div`s with `onClick` instead of a primitive.
- Magic pixel/color values scattered across the component.
