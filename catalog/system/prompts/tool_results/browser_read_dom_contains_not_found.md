---
key: tool_results.browser_read_dom_contains_not_found
version: 1
inputs: [Contains, Selector]
---
{{.Contains}} appears nowhere in the text or attributes of {{.Selector}} on this page. It is not rendered here — this is a definitive answer, do not re-read the DOM to confirm it. If you expected it, the page is stale (reload), the build did not include your change, or the element is on another route.
