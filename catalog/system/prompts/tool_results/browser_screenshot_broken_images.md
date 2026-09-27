---
key: tool_results.browser_screenshot_broken_images
version: 1
inputs: [Count, List]
---


WARNING: {{.Count}} image(s) on this page FAILED TO LOAD and render as a broken-image placeholder:
- {{.List}}
The page does NOT render correctly. Fix these before any "looks correct" verdict: the referenced asset file is missing, its path is wrong, or it is not a real image. If the asset does not exist yet, download the real one with download_file.
