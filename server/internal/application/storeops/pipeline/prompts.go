package pipeline

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// This file registers every LLM-facing string render.go renders — see
// catalog/system/guards/storeops_pipeline_*.md. Nothing here is a Go
// string literal; each Key's sample only has to be renderable, not
// realistic. It reaches an agent through storeops.Service.StartBuild,
// called from a batch/store release's deploy_release/trigger_release path
// (the failure lands on the release's ReleaseStoreBuild.Error, read back by
// get_release/watch_release).

type pipelinePlatformInput struct{ Platform string }

var unsupportedPlatformKey = prompt.Define("guard.storeops_pipeline_unsupported_platform", pipelinePlatformInput{Platform: `"web"`})

type pipelineIdentifierInput struct{ Identifier string }

var invalidIdentifierKey = prompt.Define("guard.storeops_pipeline_invalid_identifier", pipelineIdentifierInput{Identifier: `"bad id"`})

type pipelinePathInput struct{ Path string }

var invalidSubPathKey = prompt.Define("guard.storeops_pipeline_invalid_subpath", pipelinePathInput{Path: `"../x"`})

type pipelineSchemeInput struct{ Scheme string }

var iosSchemeRequiredKey = prompt.Define("guard.storeops_pipeline_ios_scheme_required", pipelineSchemeInput{Scheme: `""`})

type pipelineModuleInput struct{ Module string }

var androidModuleRequiredKey = prompt.Define("guard.storeops_pipeline_android_module_required", pipelineModuleInput{Module: `""`})
