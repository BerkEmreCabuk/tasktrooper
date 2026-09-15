package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// AgentCLIStore persists which local agent CLIs this install has connected —
// zero or more, one row per flavor. See domain.AgentCLIConnection for why an
// install may hold several at once.
type AgentCLIStore interface {
	// Get returns flavor's own connection, and false when that flavor is not
	// connected. Not connected is a normal state, not an error.
	Get(ctx context.Context, flavor domain.AgentCLIFlavor) (domain.AgentCLIConnection, bool, error)
	// List returns every flavor this install currently has connected, in no
	// particular order.
	List(ctx context.Context) ([]domain.AgentCLIConnection, error)
	// Set upserts conn's own flavor, leaving every other flavor's connection
	// untouched — connecting one no longer disconnects another.
	Set(ctx context.Context, conn domain.AgentCLIConnection) error
	// Clear removes the connection when it is flavor's, and does nothing
	// otherwise. The flavor is checked in the statement rather than by the
	// caller so a disconnect racing a connect of the SAME flavor from another
	// request cannot delete a row a concurrent connect just wrote.
	Clear(ctx context.Context, flavor domain.AgentCLIFlavor) error
}
