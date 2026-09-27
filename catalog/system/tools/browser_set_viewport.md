---
key: tool.browser_set_viewport
version: "1"
params:
    device: 'Preset to emulate: mobile (390x844, touch, mobile user agent), tablet (768x1024, touch) or desktop (1440x900, no touch)'
    height: Custom viewport height in CSS pixels; overrides the preset height
    width: Custom viewport width in CSS pixels; overrides the preset width
---
Switch the shared browser between mobile, tablet and desktop emulation — viewport size, device pixel ratio, touch support and mobile user agent — and report the responsive state of the current page: whether it scrolls horizontally and which elements overflow the viewport. The emulation persists for every later browser_* call, so read the DOM and take screenshots after switching. Call with no arguments to report the current viewport without changing it.
