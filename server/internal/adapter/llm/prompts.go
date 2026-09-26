package llm

import "github.com/makifbaysal/tasktrooper/server/internal/application/prompt"

// This file registers this package's LLM-facing strings — see
// catalog/system/prompts/llm/**.

type screenshotCountInput struct{ N int }

var (
	carriedImagesNoteKey   = prompt.Define("llm.carried_images_note", screenshotCountInput{N: 1})
	toolImagePreambleKey   = prompt.Define("llm.tool_image_preamble", screenshotCountInput{N: 1})
	jsonOnlyInstructionKey = prompt.Define("llm.json_only_instruction", struct{}{})
)
