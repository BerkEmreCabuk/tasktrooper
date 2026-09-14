package http

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

// "This needs somebody's Mac and it is not there" reaches a person two ways,
// and they are genuinely different states rather than one state reported
// twice.
//
//	A DISPATCHED BOARD RUN has nobody waiting on a socket. It parks — see
//	domain.ResourceRunnerNotAttached and board.Runner.parkOnRunner — keeps its
//	origin column, and the sweeper resumes it when that member's Mac comes
//	back. Failing it would spend one of the task's three consecutive-failure
//	lives on a closed laptop, and would put "the run failed" on a task nobody
//	started.
//
//	A USER-INITIATED REQUEST has somebody watching a spinner. It must fail at
//	once, with the code the clients switch on, because the only thing that
//	resolves it is that person walking to their laptop — and a request that
//	hung, retried, or answered "no results" would hide the one instruction
//	that ends the wait.
//
// This file is the second one. It is deliberately a check on the ERROR rather
// than a list of routes: the condition is raised deep (in the runner transport,
// or in the embedding client) and every route that propagates it needs the same
// answer, so a hand-kept list of paths would be a list that goes stale the
// first time somebody adds a handler that embeds a query.
//
// # No Retry-After, and that is the point
//
// The control plane answers its own 409 without one, deliberately: nothing
// about this becomes true by waiting, so a client that backed off and retried
// would show a spinner where the "connect your Mac" screen belongs. This
// mirrors that exactly.
const codeRunnerNotAttached = "runner_not_attached"

// runnerNotAttachedResponse is the 409 body.
//
// `code` is at the TOP level as well as in `error.type` because two different
// programs write this shape and the clients switch on the string, not on the
// path it came from: the control plane's own refusal is `{"error":…,"code":…}`
// (tenant-manager's writeJSONCode), and a client that learned to read the code
// off one of them must not have to learn a second place for the other.
type runnerNotAttachedResponse struct {
	Error errorDetail `json:"error"`
	Code  string      `json:"code"`
	// MemberUID is WHOSE Mac is missing, when it is known. Empty on the
	// embedding path, where the call is always made as the acting member and
	// there is nobody else it could have been.
	MemberUID string `json:"member_uid,omitempty"`
	// Self says the missing Mac belongs to the person who made this request.
	//
	// It is the whole team/solo distinction, and it is the most this process
	// can honestly say: only the control plane holds the tunnel registry, so
	// only it knows whether ANY Mac is attached for this tenant. What is known
	// here is whether the one we needed was the caller's own, and that is the
	// half the wording actually turns on — "open your laptop" versus "this card
	// is assigned to a colleague whose machine is offline". A client that wants
	// "nobody in the workspace is connected" has to ask the control plane.
	Self bool `json:"self"`
}

// runnerNotAttached writes the 409 when err means "no Mac", reporting whether
// it did so the caller can fall through to its ordinary error handling.
//
// It returns (handled, writeErr) rather than a single error, and that is not
// ceremony: fiber's c.JSON returns NIL on success, so a helper of this shape
// that answered with one error value would report "I did nothing" precisely
// when it had done its job — every missing Mac would fall straight through to
// the 500 this function exists to prevent, and the tests below would be the
// only thing that ever noticed.
//
// Two conditions, because the condition is raised in two places that cannot
// share a type: *domain.RunnerBlock comes back from the runner transport (a
// board run, a workspace preparation, a preflight probe), and
// domain.ErrRunnerNotAttached comes back from the embedding client, which is an
// LLM client and can only return an error. Both mean the same thing to a
// person.
//
// A NotReady block is deliberately included. To a waiting human "your Mac is
// connected but still starting" and "your Mac is not connected" are the same
// instruction — look at that machine — and the sentence on the block already
// says which it is.
func runnerNotAttached(c *fiber.Ctx, err error) (bool, error) {
	if err == nil {
		return false, nil
	}
	block, isBlock := domain.RunnerBlockOf(err)
	if !isBlock && !errors.Is(err, domain.ErrRunnerNotAttached) {
		return false, nil
	}

	// The acting member. On the embedding path the call was made AS this
	// person, so a missing Mac there is always their own; on the transport path
	// the block names the member it tried to reach, which on a team may be
	// somebody else's card.
	actor := tenant.UserID(c.UserContext())
	member, message := "", ""
	if isBlock {
		member = block.MemberUID
		message = block.UserMessage(acceptLanguage(c))
	} else {
		message = err.Error()
	}
	self := member == "" || member == actor

	return true, c.Status(fiber.StatusConflict).JSON(runnerNotAttachedResponse{
		Error:     errorDetail{Message: message, Type: codeRunnerNotAttached},
		Code:      codeRunnerNotAttached,
		MemberUID: member,
		Self:      self,
	})
}

// acceptLanguage picks the language RunnerBlock.UserMessage answers in. Only
// the primary tag is read and only Turkish is recognised, because those are the
// two strings that exist; anything else is English, which is the same rule the
// quota block's message already follows.
func acceptLanguage(c *fiber.Ctx) string {
	if lang := c.Get("Accept-Language"); len(lang) >= 2 && (lang[:2] == "tr" || lang[:2] == "TR") {
		return "tr"
	}
	return ""
}
