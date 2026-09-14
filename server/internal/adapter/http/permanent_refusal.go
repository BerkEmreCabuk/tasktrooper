package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/catalog"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// A refusal that knows exactly why it is refusing, and that no retry can turn
// into a success, must not leave through the generic error path.
//
// Four of them did: activating cursor_agent or antigravity (declared, no
// executor written), and connecting or activating claude_code (a process on a
// machine, not an endpoint on the network). Each answered 500 with a correct,
// permanent sentence in the body — and 500 is read by every client and every
// monitor as "we broke, back off and send it again", which is the one thing
// none of these should do.
//
// 409, and the codes below, match tenant-manager: its onboarding route already
// answers `provider_unavailable` with 409 for the very same refusal
// (internal/control/gateway/gateway_onboarding.go), and a refusal that gets one
// number from the control plane and a different one from this server is a
// refusal the web app has to learn twice.
//
// Checked on the ERROR rather than per route, for the reason runner_not_attached.go
// gives for doing the same: these are raised deep in the provider service and
// every route that lets one out owes the same answer, so a hand-kept list of
// paths would go stale the first time somebody added a handler.
const (
	// codeProviderUnavailable: a declared-but-not-built provider was named.
	// Same string tenant-manager writes.
	codeProviderUnavailable = "provider_unavailable"
	// codeHostExecutedProvider: a provider that is executed as a process was
	// asked to behave like an endpoint.
	codeHostExecutedProvider = "host_executed_provider"
)

// codeAssigneeNotMember: a task was given to somebody the workspace roster does
// not have. A 400 rather than a 409 — the assignee in the body is wrong, and
// the fix is a different body — but a MACHINE-READABLE one, which it was not:
// the web app was matching a substring of the sentence to recognise it, so
// rewording the explanation changed client behaviour. See typedBadRequests.
const codeAssigneeNotMember = "assignee_not_member"

// codeInvalidCatalogInput: agent-catalog validation — a missing name, a skill
// with no content, an effort level that is not one of the five. See
// catalog.ErrInvalidInput, which already carries the sentence.
const codeInvalidCatalogInput = "invalid_catalog_input"

// codeUnknownAgentCLIFlavor: a flavor that names no CLI at all, as opposed to
// one that is known and unavailable — which is codeProviderUnavailable and a
// 409. The two need different words from the client, so they need different
// codes.
const codeUnknownAgentCLIFlavor = "unknown_agent_cli_flavor"

// permanentRefusals maps each sentinel to the code clients switch on.
var permanentRefusals = []struct {
	sentinel error
	code     string
}{
	{domain.ErrProviderUnavailable, codeProviderUnavailable},
	{domain.ErrHostExecutedUnservable, codeHostExecutedProvider},
}

// typedBadRequests are 400s that are worth branching on rather than reading.
//
// The status is already right for these; what was missing is the `type`. A
// refusal with no code leaves a client only the prose, and a client that
// matches on prose breaks the next time somebody improves the sentence — which
// is exactly the coupling a code exists to prevent.
var typedBadRequests = []struct {
	sentinel error
	code     string
}{
	{domain.ErrAssigneeNotMember, codeAssigneeNotMember},
	{catalog.ErrInvalidInput, codeInvalidCatalogInput},
}

// codedBadRequest is badRequest for a refusal this handler recognises itself,
// with no sentinel to reach typedBadRequest by.
func codedBadRequest(c *fiber.Ctx, code, msg string) error {
	return c.Status(fiber.StatusBadRequest).JSON(codedErrorResponse{
		Error: errorDetail{Message: msg, Type: code},
		Code:  code,
	})
}

// typedBadRequest writes the coded 400 when err is one of them.
func typedBadRequest(c *fiber.Ctx, err error) (bool, error) {
	if err == nil {
		return false, nil
	}
	for _, r := range typedBadRequests {
		if !errors.Is(err, r.sentinel) {
			continue
		}
		return true, c.Status(fiber.StatusBadRequest).JSON(codedErrorResponse{
			Error: errorDetail{Message: err.Error(), Type: r.code},
			Code:  r.code,
		})
	}
	return false, nil
}

// codedErrorResponse carries the code at the TOP level as well as in
// error.type, for the reason runnerNotAttachedResponse does: tenant-manager
// writes `{"error":…,"code":…}` and a client that learned to read the code off
// one of the two programs must not have to learn a second place for the other.
type codedErrorResponse struct {
	Error errorDetail `json:"error"`
	Code  string      `json:"code"`
}

// permanentRefusal writes the 409 when err is one, reporting whether it did so
// the caller can fall through to its ordinary handling.
//
// It returns (handled, writeErr) rather than one error for the reason
// runnerNotAttached documents: fiber's c.JSON returns nil on success, so a
// single-error helper would report "I did nothing" exactly when it had done its
// job.
func permanentRefusal(c *fiber.Ctx, err error) (bool, error) {
	if err == nil {
		return false, nil
	}
	for _, r := range permanentRefusals {
		if !errors.Is(err, r.sentinel) {
			continue
		}
		log.Info().Str("path", c.Path()).Str("method", c.Method()).Str("code", r.code).
			Msg("permanent refusal")
		return true, c.Status(fiber.StatusConflict).JSON(codedErrorResponse{
			Error: errorDetail{Message: err.Error(), Type: r.code},
			Code:  r.code,
		})
	}
	return false, nil
}
