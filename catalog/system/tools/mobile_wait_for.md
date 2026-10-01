---
key: tool.mobile_wait_for
version: "1"
params:
    content_desc: Accessibility label (content-desc) of the element
    resource_id: Android resource-id, with or without the package prefix (e.g. "login_button")
    text: Visible label of the element, matched exactly (e.g. "Sign in")
    timeout_seconds: 'How long to wait (default: 10, max: 60)'
    xpath: XPath over the hierarchy from mobile_read_ui. Last resort — prefer the other three, they survive layout changes
---
Wait until an element appears on the connected device (Android or iOS simulator). Use it after a tap that starts a load, instead of taking a screenshot and hoping the screen has settled.
