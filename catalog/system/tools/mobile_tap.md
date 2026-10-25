---
key: tool.mobile_tap
version: "1"
params:
    content_desc: Accessibility label (content-desc) of the element
    resource_id: Android resource-id, with or without the package prefix (e.g. "login_button")
    text: Visible label of the element, matched exactly (e.g. "Sign in")
    x: Absolute x pixel; only with y, and only when no selector can address the target
    xpath: XPath over the hierarchy from mobile_read_ui. Last resort — prefer the other three, they survive layout changes
    "y": Absolute y pixel; only with x
---
Tap an element on the connected device (Android or iOS simulator). Address it by text, resource_id or content_desc; x/y is a fallback for canvases and maps.
