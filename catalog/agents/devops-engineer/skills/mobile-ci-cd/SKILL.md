---
name: mobile-ci-cd
category: ci-cd
description: Use when automating an iOS or Android build and release - fastlane lanes, code signing and provisioning, App Store Connect and Play Console uploads, build numbering, beta tracks, and CI runners for mobile
---

# Mobile CI/CD

## Overview

**Core principle:** Signing identities and store credentials live in a secret store and are installed on a throwaway CI keychain for the length of one build. Nothing is installed by hand on a runner and nothing is committed.

Mobile pipelines fail differently from server ones: the failure is almost always signing, a version/build number, or a store rule — not compilation.

## fastlane is the unit of work

Keep the build logic in `fastlane/Fastfile` so the same command runs locally and in CI. A workflow that inlines 40 lines of `xcodebuild` flags cannot be reproduced by a human debugging it.

```ruby
platform :ios do
  lane :beta do
    setup_ci                                   # throwaway keychain on CI
    match(type: "appstore", readonly: true)    # certificates from the encrypted repo/storage
    # The App Store Connect API key, assembled from three CI secrets. The lane
    # context carries it to every later step that talks to ASC.
    app_store_connect_api_key(
      key_id: ENV["ASC_KEY_ID"],
      issuer_id: ENV["ASC_ISSUER_ID"],
      key_content: ENV["ASC_KEY_P8"],          # the .p8 contents
      is_key_content_base64: true
    )
    # Never GITHUB_RUN_NUMBER on its own: it does NOT change on a re-run, and
    # both stores reject a build number that was already uploaded.
    increment_build_number(build_number: ENV["GITHUB_RUN_NUMBER"].to_i * 100 + ENV["GITHUB_RUN_ATTEMPT"].to_i)
    build_app(scheme: "Shop", export_method: "app-store")
    upload_to_testflight(skip_waiting_for_build_processing: true)
  end
end
```

## iOS signing

| Thing | Where it belongs |
|---|---|
| Certificates + provisioning profiles | `match` (encrypted git repo or cloud storage), or a signing certificate stored as a CI secret and imported per run |
| App Store Connect API key | Three CI secrets (key id, issuer id, `.p8` contents) passed to `app_store_connect_api_key`; never committed, never printed. `upload_to_testflight`'s `api_key_path` expects fastlane's ASC key **JSON**, not a raw `.p8` — passing the `.p8` there fails after the build has already run |
| Keychain | Created by `setup_ci` per run; never the runner's login keychain |
| Xcode version | Pinned in the workflow (`xcode-version:` / `xcodes select`), matching what the team builds with |

Automatic signing on CI is fragile; explicit profiles are reproducible. A failed build that mentions "no matching provisioning profile" is a profile/bundle-id/capability mismatch, not a transient error.

## Android signing

The upload keystore is a CI secret (base64) decoded to a temp file; `storePassword`/`keyPassword`/`keyAlias` are separate secrets read by `build.gradle`. Never in `gradle.properties` in the repository. Publishing uses a Play service-account JSON, also a secret, with `upload_to_play_store(track: "internal")`.

## Versioning

Marketing version (`1.4.0`) is a product decision and lives in the repository. Build number is CI's, and it must increase on every upload INCLUDING a re-run of the same job: `GITHUB_RUN_NUMBER` stays the same on a re-run (only `GITHUB_RUN_ATTEMPT` changes), so combine the two (`run_number * 100 + run_attempt`), use the commit count, or ask the store for the last one (`latest_testflight_build_number` + 1).

## Runners

iOS needs macOS (GitHub's `macos-latest`, or a self-hosted Mac). Cache Pods/SPM and Gradle to keep builds under ten minutes. Set a job timeout — a stuck simulator otherwise burns the whole runner quota. Store the `.ipa`/`.aab` and the build log as artifacts.

## Tracks and review

Ship to TestFlight / Play internal track from CI automatically; promotion to production is a human decision with a store review in between. Never wire "push to main" straight to a production store release.

## Red flags

- A `.p12`, `.p8`, `.jks` or `google-services.json` with real credentials committed to the repository.
- `fastlane match` in read/write mode from CI (it can regenerate and revoke the team's certificates).
- A build number derived from the date with a resolution that can repeat within one day.
- Secrets echoed by a `set -x` shell step in the middle of a signing lane.
