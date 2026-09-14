package mcp

import (
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

// urlPolicy is read per call rather than cached in the Service, because
// validateTransportFields is a package-level function shared by create and
// update and has nowhere to hang state. urlguard.Default only reads one
// environment variable, so this stays cheap.
var urlPolicy = urlguard.Default

func validateCreateRequest(req domain.CreateMCPServerRequest) error {
	if strings.TrimSpace(req.ID) == "" {
		return fmt.Errorf("%w: id is required", domain.ErrMCPInvalidRequest)
	}
	if strings.ContainsAny(req.ID, " /") {
		return fmt.Errorf("%w: id must not contain spaces or slashes", domain.ErrMCPInvalidRequest)
	}
	return validateTransportFields(req.Transport, req.Command, req.URL)
}

func validateUpdateRequest(id string, req domain.UpdateMCPServerRequest) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: id is required", domain.ErrMCPInvalidRequest)
	}
	return validateTransportFields(req.Transport, req.Command, req.URL)
}

func validateTransportFields(transport, command, url string) error {
	if transport == "" {
		transport = "stdio"
	}
	switch transport {
	case "stdio":
		if strings.TrimSpace(command) == "" {
			return fmt.Errorf("%w: stdio transport requires command", domain.ErrMCPInvalidRequest)
		}
	case "http":
		if strings.TrimSpace(url) == "" {
			return fmt.Errorf("%w: http transport requires url", domain.ErrMCPInvalidRequest)
		}
		// Non-emptiness was the whole check, and it is not enough. An agent
		// configures this URL, and cfg.Headers next to it is an arbitrary
		// attacker-chosen map — the combination is what satisfies GCP's
		// Metadata-Flavor: Google gate that fetch_url cannot reach. Reject the
		// obvious internal destinations before the row is ever stored.
		//
		// Precheck, not Validate: nothing is resolved here. A write-time DNS
		// lookup would reject an endpoint whose name is not up yet, and it
		// would prove nothing anyway, since the row is dialled minutes or days
		// later and the answer can change in between. The binding check is at
		// connect time in adapter/mcp/client.go, which also covers rows that
		// were stored before this check existed.
		if _, err := urlPolicy().Precheck(url); err != nil {
			return fmt.Errorf("%w: mcp url is not an allowed destination", domain.ErrMCPInvalidRequest)
		}
	default:
		return fmt.Errorf("%w: unsupported transport %q (use stdio or http)", domain.ErrMCPInvalidRequest, transport)
	}
	return nil
}
