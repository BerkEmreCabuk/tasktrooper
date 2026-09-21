---
name: frontend-manual-testing
category: qa
description: Boot the frontend, drive the changed flows with the browser tools, capture screenshots at each step, and run a visual inspection checklist
---
# Frontend Manual Testing

## Overview

A frontend task is verified by rendering it: boot the app, drive the changed flows in a real browser, and look at what the user would see. Screenshots are mandatory evidence — a UI verdict without images is an untested claim.

**Preconditions:** scenario list posted first (scenario-plan-first) covering both functional cases (per AC) and visual cases (layout, states, responsiveness); environment chosen per test-environment-selection.

## Booting

- Install and start per the project's declared commands (`npm ci` + dev/preview script, or the repo's `build_command`). Point the app at the local backend you booted (backend-manual-testing) or at the stage API base URL — the API base is usually an env var (`VITE_API_URL` or similar); read the project's config, don't guess.
- Confirm the page actually loads before scenario work: a blank page with console errors is finding #1.

## Driving flows and taking screenshots

The browser tools are the primary way to drive the app — no local Chromium setup, no script files:

1. `browser_navigate` to the page under test.
2. `browser_wait_for` the element or text that proves the page rendered (an eternal spinner or a timeout here is finding #1 — never screenshot a page you have not waited on).
3. `browser_screenshot` the screen at desktop, then `browser_set_viewport` with `device: "mobile"` and screenshot it again — for every changed screen. The viewport switch emulates size, touch and the mobile user agent together, and answers the responsive question in words: whether the page scrolls sideways and which elements overflow. Report what it says; horizontal overflow is a finding.

Interactive flows (login, forms, dialogs): `browser_fill` each field, `browser_click` the submit or action element, `browser_wait_for` the post-action state (URL, toast, new element), then `browser_screenshot` the result. Use `browser_read_dom` for what a screenshot cannot prove — an input's actual value, a disabled/aria state, the exact error text.

Screenshot every meaningful step of every scenario, at desktop and — via `browser_set_viewport` — at mobile, for changed screens.

Fallback — only when you are building the automation suite (e2e-automation-project) or the browser tools are unavailable — drive the system's headless Chromium (`$CHROME_BIN`, `/usr/bin/chromium`, `--no-sandbox` in containers) via `run_terminal`:

```bash
mkdir -p qa-evidence
chromium --headless=new --no-sandbox --disable-gpu --hide-scrollbars \
  --window-size=1440,900 --screenshot=qa-evidence/01-board-desktop.png http://localhost:5173/board
chromium --headless=new --no-sandbox --disable-gpu --hide-scrollbars \
  --window-size=390,844 --screenshot=qa-evidence/01-board-mobile.png http://localhost:5173/board
```

For scripted interactive flows in that fallback, use Playwright with the system browser (no browser download):

```js
// qa-evidence/flow.mjs — run with: node qa-evidence/flow.mjs
import { chromium } from 'playwright-core';
const browser = await chromium.launch({
  executablePath: process.env.CHROME_BIN || '/usr/bin/chromium',
  args: ['--no-sandbox'],
});
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
const consoleErrors = [];
page.on('console', (m) => m.type() === 'error' && consoleErrors.push(m.text()));
await page.goto('http://localhost:5173/login');
await page.fill('[name=email]', 'qa@example.com');
await page.fill('[name=password]', 'qa-pass');
await page.click('button[type=submit]');
await page.waitForURL('**/board');
await page.screenshot({ path: 'qa-evidence/02-after-login.png', fullPage: true });
console.log('console errors:', consoleErrors);
await browser.close();
```

## Visual inspection checklist

Look at each screenshot deliberately — per image, answer:

- Overflowing/clipped/truncated text, overlapping elements, broken alignment or spacing?
- Broken images or icons, missing fonts, unreadable contrast?
- Empty state, loading state, and error state each render intentionally (not a blank area or eternal spinner)?
- Mobile width: nothing unusable, no horizontal scroll?
- Console: zero uncaught errors during the flow (capture them in the driver script when on the Playwright fallback). Failed network calls in the flow are findings.
- Copy: right language for the app's locale, no placeholder text left in.

## Evidence

List every screenshot path in the verdict comment with one line of what it shows and whether it is OK ("02-after-login.png — board renders after login, columns intact"). Console error summary included. Findings become need_revision items with the screenshot as reproduction evidence (bug-report-writing).

After the manual pass, the same flows are added to the UI automation suite (e2e-automation-project) and wired into the pipeline (automation-pipeline-integration).

## Red Flags

- A UI verdict formed from the diff or from "the build passed" → you have not seen the pixels.
- Screenshots only at desktop width for a layout-affecting change.
- A stuck spinner or blank section dismissed as "probably my environment" — reproduce or report it, never ignore it.
