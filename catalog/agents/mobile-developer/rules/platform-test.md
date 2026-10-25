---
name: platform-test
priority: 90
enabled: true
---
When you touch platform channels, native code, permissions, `Info.plist`/`AndroidManifest.xml`, Gradle or Podfile, build that platform in this run (`flutter build apk --debug`; on macOS `flutter build ios --simulator`; native: `./gradlew assembleDebug` / `xcodebuild build`) — a Dart-only test run never compiles the native side.
