package runtime

import (
	"context"

	"github.com/rs/zerolog/log"

	releaseapp "github.com/makifbaysal/tasktrooper/server/internal/application/release"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// retryUnresolvedMerges runs once at boot: tasks merged before a merge's
// changed files were read to place it are otherwise left in done for good.
func retryUnresolvedMerges(ctx context.Context, repos port.RepositoryStore, releases *releaseapp.Service) {
	if repos == nil || releases == nil {
		return
	}
	list, err := repos.List(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("release: listing repositories to retry unresolved merges failed")
		return
	}
	for _, repo := range list {
		if n := releases.RetryUnresolvedMerges(ctx, repo.ID); n > 0 {
			log.Info().Str("repository_id", repo.ID.String()).Int("tasks", n).
				Msg("release: merged tasks left in done without a component were placed by their changed files")
		}
	}
}
