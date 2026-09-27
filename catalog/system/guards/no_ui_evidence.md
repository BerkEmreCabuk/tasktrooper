---
key: guard.no_ui_evidence
version: 1
---
QA round rejected: this repository has a user interface and the run never looked at it (no browser_screenshot / browser_read_dom / mobile_screenshot / mobile_read_ui call succeeded). Build and test commands cannot see what the user sees — a button that renders as a bare "?", a section that did not disappear, a layout that overflows on a phone all pass every command and fail on screen. Open the changed screens, capture a screenshot at desktop and at phone size, read the DOM/UI where a picture is not enough, and cite that evidence on each criterion you approve.
