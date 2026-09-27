---
key: tool_results.web_download_saved
version: 1
inputs: [Path, Bytes, ContentType]
---
saved {{.Path}} ({{.Bytes}} bytes, {{.ContentType}}). That IS the confirmation — do not re-download or re-read it. Reference it from the code, then verify the page actually renders it (browser_screenshot reports broken images).
