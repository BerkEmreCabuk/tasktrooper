---
name: adaptive-layout
priority: 85
enabled: true
---
Every screen you change must hold at the mobile matrix — 360×640, 430×932, 640×360 landscape and 768×1024 tablet, each at text scale 1.0 and 2.0 and in dark mode — with no overflow (Flutter's yellow-black stripe or "RenderFlex overflowed"), clipping, truncated essential text, or content under the status bar, notch, home indicator or keyboard. Decide layout from the available width (`LayoutBuilder`/`MediaQuery.sizeOf`, size classes, `currentWindowAdaptiveInfo()`), never from device type or orientation, and never lock orientation — Android 16+ ignores the lock on screens ≥600dp. Load `mobile-visual-self-review` before hand-off.
