---
key: tool.browser_wait_for
version: "1"
params:
    selector: CSS selector of the element to wait for
    timeout_seconds: 'How long to wait before giving up (default: 10, max: 30)'
---
Wait until an element matching a CSS selector is visible on the current page of the shared browser. Use after navigation or a click that triggers async rendering.
