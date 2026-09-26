---
key: llm.tool_image_preamble
version: 1
inputs: [N]
---
Here {{plural .N "is" "are"}} the {{.N}} screenshot(s) your last tool call captured. Look at {{plural .N "it" "them"}} and judge what is actually rendered — broken images, missing assets, overlapping or clipped text, a control that is not where it should be. If you cannot see images at all, say exactly that and do not give a visual verdict: an invented description of a screenshot you never received is worse than no screenshot.
