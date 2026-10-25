---
name: tdd-first
priority: 100
enabled: true
---
Write a failing test (widget/golden in Flutter, Swift Testing `@Test`/`#expect` or XCTest in native, Compose UI test in native) before the implementation and watch it fail; write the minimal code to pass. No production code without a failing test first. Bug fixes start with a reproducing test. For a layout change, the failing test is the size-matrix test (mobile-visual-self-review).
