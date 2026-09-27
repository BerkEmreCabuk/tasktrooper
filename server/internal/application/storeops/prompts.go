package storeops

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// This file registers the LLM-facing strings this package renders — see
// catalog/system/prompts/briefs/storeops/**. Nothing here is a Go string
// literal.

var blockedReleaseRemedyKey = prompt.Define[struct{}]("briefs.storeops.blocked_release_remedy", struct{}{})

// buildTargetIOSSchemeKey/buildTargetAndroidModuleKey are engine.go's
// buildTargetRemedy — the build.Error a batch/store deploy leaves on
// ReleaseStoreBuild when the repository does not state a build target,
// read back by get_release/watch_release.
var buildTargetIOSSchemeKey = prompt.Define[struct{}]("guard.storeops_build_target_ios_scheme", struct{}{})
var buildTargetAndroidModuleKey = prompt.Define[struct{}]("guard.storeops_build_target_android_module", struct{}{})
