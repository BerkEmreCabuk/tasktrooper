---
key: tool_results.browser_read_dom_empty_text
version: 1
inputs: [HTMLLen, Visibility]
---
The element matched but its rendered text is empty (outer HTML is {{.HTMLLen}} chars, element {{.Visibility}}). Rendered text never includes hidden elements, icon-only buttons or attribute values — an empty read is NOT proof the content is missing. Call again with as_text:false to read the HTML, or with contains:"<what you are looking for>" to search text and attributes.
