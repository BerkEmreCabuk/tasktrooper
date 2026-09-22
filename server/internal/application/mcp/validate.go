package mcp

import (
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

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

		if _, err := urlPolicy().Precheck(url); err != nil {
			return fmt.Errorf("%w: mcp url is not an allowed destination", domain.ErrMCPInvalidRequest)
		}
	default:
		return fmt.Errorf("%w: unsupported transport %q (use stdio or http)", domain.ErrMCPInvalidRequest, transport)
	}
	return nil
}
