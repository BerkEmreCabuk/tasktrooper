---
name: mobile-manual-testing
category: qa
description: Verify a mobile task by building it and exercising every layer the environment can actually reach, and by reporting the layers it cannot instead of approving them
---
# Mobile Manual Testing

## Overview

A mobile task is verified from the outside like any other: you run something and observe what it does. What differs is the environment. The QA container is Linux and has no iOS simulator, and an Android emulator needs hardware virtualisation that is usually absent — so a mobile round has a real ceiling, and the job is to test everything under it and be exact about what is above it.

Never approve a criterion because the code looks right. An untestable criterion is reported, not passed.

## What is always testable

1. **The build.** Build the app for its platform from the task branch — `flutter build apk --debug`, `./gradlew assembleDebug`, `xcodebuild -scheme … build` (macOS runners only), whatever the repo's own scripts declare. A build failure is a `need_revision` finding on its own, with the compiler output quoted.
2. **Static analysis the repo already runs.** `flutter analyze`, `./gradlew lint`, `swiftlint` — only where the repo defines them. New errors on the branch are findings.
3. **The app's own automated tests, run as they are.** `flutter test`, `./gradlew testDebugUnitTest` (this is running what the developer wrote — it is not the authoring of a new suite, which is out of scope this iteration).
4. **The API side of every flow the task touches.** Most mobile criteria are "the app shows X after calling Y": call Y yourself with curl against the task branch's backend or the stage target (`get_deploy_target`), and verify the payload the app would render. That converts a large part of a mobile criterion into an executed check.
5. **Anything the repo can render headlessly.** A Flutter web build of the same screens (`flutter build web` / `flutter run -d web-server`) is driven with the browser tools like any web app, and its screenshots are evidence for layout and copy — state it explicitly as web-rendered, since it is not proof of native behaviour.

## What is usually NOT testable here, and what to do about it

Real-device behaviour — touch gestures, native permissions, push delivery, biometrics, store flows, platform look and feel — needs a device or emulator this environment does not have.

For each such criterion, write in the verdict comment: the criterion, why it could not be executed (`no iOS simulator on a Linux runner`, `emulator start failed: /dev/kvm missing`), and what would unblock it (a macOS runner, a device farm, a TestFlight build a human can drive). Then leave it unapproved. `review_criterion` with `approved=false` and that note is the honest record; approving it silently is the failure this skill exists to prevent.

If the emulator DOES start (`emulator -avd … -no-window`, `adb devices` shows it), then treat it exactly like a frontend round: drive the flows, `adb exec-out screencap -p > shot.png` for evidence, and report per scenario.

## Reporting

One comment, per scenario: what you ran, what you observed, PASS or FAIL — followed by an explicit **Not verified in this environment** list. A mobile round with an empty second list is only believable when a device actually ran.
