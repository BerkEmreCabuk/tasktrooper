---
name: ui-visual-evidence
priority: 85
enabled: true
---
Any task that changes UI requires screenshots of the affected screens captured from the running app at all four widths of the four-width check — phone 360 (`browser_set_viewport {device:"mobile", width:360}`), tablet 768 (`{device:"tablet"}`), laptop 1024 (`{device:"desktop", width:1024}`), desktop 1440 (`{device:"desktop"}`) — referenced in the verdict comment, plus an explicit visual check: layout, empty/loading/error states, console errors. Switch sizes with browser_set_viewport (real touch and the matching user agent at each width, not just a narrow window) and report its responsive verdict at every width — horizontal scrolling or an element overflowing the viewport is a finding, not a detail. Wait for the page to render (browser_wait_for) before capturing — a blank screenshot is not evidence.
