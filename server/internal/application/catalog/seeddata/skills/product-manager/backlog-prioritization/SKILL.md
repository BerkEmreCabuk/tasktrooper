---
name: backlog-prioritization
category: pm
description: Use when ordering board tasks - score with RICE, apply the fixed overrides (bugs, unblockers, stakeholder urgency), and place items in backlog vs todo accordingly
---

# Backlog Prioritization (RICE)

## Overview

Prioritization decides what agents pick up next. RICE gives a defensible default order; a few overrides trump the score because some work is urgent or unblocks everything else.

**Core principle:** RICE for the default order; overrides for bugs, unblockers, and explicit urgency.

## RICE

`score = (Reach × Impact × Confidence) ÷ Effort` — higher first.

| Factor | Meaning |
|--------|---------|
| Reach | how many users/flows affected |
| Impact | 3 massive / 2 high / 1 medium / 0.5 low |
| Confidence | % certainty in the estimates |
| Effort | developer-days |

## Overrides (regardless of RICE)

1. **Bugs before features** — severity order: data loss > broken flow > degraded UX > cosmetic.
2. **Unblocking dependencies** — an analiz result unlocks multiple tasks, so schedule analiz early even if its own RICE is modest.
3. **Explicit stakeholder urgency.**

## Column meaning

- **backlog:** lower-priority or not-yet-ready items.
- **todo:** ready for agent pickup now.

## Worked Example

Three candidates:
- A: new dark mode — Reach 200, Impact 1, Conf 80%, Effort 4 → RICE 40.
- B: analiz for reporting — Reach 500 (unlocks 3 tasks), Impact 2, Conf 60%, Effort 2 → RICE 300, and it's an unblocker.
- C: data-loss bug on task delete — any RICE.

Order: **C first** (data-loss bug override), **B next** (highest RICE + unblocker), **A last**. C and B move to `todo`; A stays in `backlog`.

## Common Mistakes

- Letting a shiny feature jump a data-loss bug.
- Deferring analiz because its own RICE looks small — it unblocks others.
- Everything dumped in `todo` with no ordering.

## Red Flags

- A known data-loss bug sitting below a feature.
- An unblocking analiz scheduled late.
