---
key: tool.browser_navigate
version: "1"
params:
    url: The full URL to open (must start with http:// or https://)
---
Open a URL in the shared headless browser. Returns the page title, the final URL after redirects and a short text summary of the page. The browser session persists across browser_* calls, so later clicks and screenshots act on this page.
