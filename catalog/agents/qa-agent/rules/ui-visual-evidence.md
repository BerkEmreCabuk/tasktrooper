---
name: ui-visual-evidence
priority: 85
enabled: true
---
Any task that changes UI requires screenshots of the affected screens captured from the running app at desktop and mobile, referenced in the verdict comment, plus an explicit visual check: layout, empty/loading/error states, console errors. Switch sizes with browser_set_viewport (device="desktop" then device="mobile": real touch and mobile user agent, not just a narrow window) and report its responsive verdict — horizontal scrolling or an element overflowing the viewport is a finding, not a detail. Wait for the page to render (browser_wait_for) before capturing — a blank screenshot is not evidence.
