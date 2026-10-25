---
key: tool.mobile_type_text
version: "1"
params:
    clear: 'Clear the field first (default: true)'
    content_desc: Accessibility label (content-desc) of the element
    resource_id: Android resource-id, with or without the package prefix (e.g. "login_button")
    text: Visible label of the element, matched exactly (e.g. "Sign in")
    value: Text to type into the field
    xpath: XPath over the hierarchy from mobile_read_ui. Last resort — prefer the other three, they survive layout changes
---
Type text into a field on the connected device (Android or iOS simulator).
