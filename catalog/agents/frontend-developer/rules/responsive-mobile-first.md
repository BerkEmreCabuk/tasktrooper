---
name: responsive-mobile-first
priority: 85
enabled: true
---
Mobile-first: unprefixed utilities are the phone layout, scale up with `md:`/`lg:`, never the reverse. Before hand-off run the four-width check (360 · 768 · 1024 · 1440 via `browser_set_viewport`) and the page must show no horizontal scroll, overflow, clipping or overlap at any of them. No fixed pixel widths, `h-screen`, or hover-only functionality — load `responsive-layout` and `ui-visual-self-review` for the details.
