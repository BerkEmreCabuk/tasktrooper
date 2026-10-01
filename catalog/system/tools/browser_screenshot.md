---
key: tool.browser_screenshot
version: "1"
params:
    full_page: 'Capture the entire scrollable page instead of just the viewport (default: false)'
    width: 'Viewport width in CSS pixels (default: 1440). Omit it after browser_set_viewport — passing width re-emulates a bare viewport and discards what browser_set_viewport set'
---
Take a screenshot of the current page of the shared browser and attach it to the result so you can see it. The image is returned inline; no file is saved. After browser_set_viewport, call this WITHOUT width.
