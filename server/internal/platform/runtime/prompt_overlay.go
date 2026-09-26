package runtime

import (
	"context"
	"fmt"
	"os"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/catalogrepo"
	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
)

// loadDefaultPromptLibrary forces the embedded prompt library to parse
// before the listener opens. prompt.Default() panics (naming the broken
// file) rather than returning an error — a broken prompt is a build defect
// — so this converts that panic into a plain startup error, matching how
// every other boot failure here surfaces through Run's own return value.
func loadDefaultPromptLibrary() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("prompt library: %v", r)
		}
	}()
	prompt.Default()
	return nil
}

// reloadPromptOverlay is called after every successful agent catalog sync.
// AGENT_CATALOG_REPO pointed at a local directory (not a git URL) may carry
// its own system/ tree overriding the embedded prompts; a git-cloned source
// never reaches SyncFromCatalog's local-dir semantics, so this only ever
// applies in local development. It never fails the sync: any problem is a
// warning, and the embedded library keeps serving.
func reloadPromptOverlay(ctx context.Context, source string) {
	st, statErr := os.Stat(source)
	if statErr != nil || !st.IsDir() {
		return
	}
	src := &catalogrepo.PromptDir{Dir: source}
	fsys, label, err := src.Open(ctx)
	if err != nil {
		return
	}
	lib, err := prompt.LoadFS(fsys)
	if err != nil {
		log.Warn().Err(err).Str("source", label).Msg("prompt overlay: parse failed, keeping embedded prompts")
		return
	}
	for _, k := range prompt.DefinedKeys() {
		if _, err := k.RenderWith(lib); err != nil {
			log.Warn().Err(err).Str("source", label).Str("key", k.Name).
				Msg("prompt overlay: key failed to render its sample, keeping embedded prompts")
			return
		}
	}
	prompt.SetDefault(lib)
	log.Info().Str("source", label).Msg("prompt overlay loaded")
}
