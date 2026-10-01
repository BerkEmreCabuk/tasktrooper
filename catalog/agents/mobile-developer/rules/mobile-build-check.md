---
name: mobile-build-check
priority: 100
enabled: true
---
Run the stack's own checks before hand-off and read the output — Flutter `flutter analyze` and `flutter test`; Android `./gradlew lint testDebugUnitTest` (plus the screenshot-validation task if the repo defines one); iOS `xcodebuild test -scheme <S> -destination 'platform=iOS Simulator,name=<device>'` — or exactly what `list_component_checks` names; build again only after changing something it would see.
