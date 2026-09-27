package storeops

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// This file registers the LLM-facing strings this package renders — see
// catalog/system/prompts/briefs/storeops/**. Nothing here is a Go string
// literal.

var blockedReleaseRemedyKey = prompt.Define[struct{}]("briefs.storeops.blocked_release_remedy", struct{}{})
