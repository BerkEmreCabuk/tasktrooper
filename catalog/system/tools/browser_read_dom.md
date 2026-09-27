---
key: tool.browser_read_dom
version: "1"
params:
    as_text: 'true returns rendered innerText, false returns outer HTML (default: true)'
    contains: Search the element's subtree for this string in text and attributes instead of dumping it. Returns each match with its selector, visibility and surrounding HTML — the reliable way to check whether something is on the page.
    selector: 'CSS selector of the element to read (default: body)'
---
Read the current page of the shared browser. Returns the page URL and title, then either the rendered text (default), the outer HTML (as_text:false), or — with contains — every element whose text or attributes hold a string, each with its selector and whether it is actually visible. Use contains to answer "did the thing I added render?": rendered text does not include hidden elements or icon-only buttons, so an empty text read is not proof of absence. Output is truncated at 100KB.
