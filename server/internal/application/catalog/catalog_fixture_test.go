package catalog

import (
	"context"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/catalogrepo"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// monorepoCatalog is the repo-root catalog this package's definitions moved
// to. Reading it back through the production reader keeps the generator's
// output and the code's expectations from drifting.
const monorepoCatalog = "../../../../catalog"

func repoCatalogAgents(t *testing.T) map[string]domain.UpstreamAgent {
	t.Helper()
	agents, _, err := (&catalogrepo.Reader{Source: monorepoCatalog}).ReadCatalog(context.Background())
	if err != nil {
		t.Skipf("monorepo catalog unavailable (%v); regenerate it and rerun", err)
	}
	bySlug := make(map[string]domain.UpstreamAgent, len(agents))
	for _, a := range agents {
		bySlug[a.Slug] = a
	}
	return bySlug
}
