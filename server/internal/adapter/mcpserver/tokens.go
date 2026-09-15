package mcpserver

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Run is everything one live claude_code session needs for its tool calls to be
// executed AS the board run that started it.
//
// Ctx is the board runner's own runCtx — the exact context it would have handed
// the agent loop (application/board/runner.go: workspace dir, repository id,
// agent id, task id, branch, session id, the toolchain overlay, the tool-usage
// and token accumulators, the activity recorder). Holding a context in a struct
// is normally wrong, and it is deliberate here for two reasons:
//
//   - that context IS the attribution. Copying its values out field by field
//     would reproduce today's list and silently drop the next value the runner
//     adds, which would show up as tool calls that stop appearing in a run's
//     ledger rather than as a compile error;
//   - the token's lifetime is the run's lifetime. A call that arrives after the
//     run was cancelled must fail, and inheriting the run's cancellation is what
//     makes that happen without a second mechanism.
type Run struct {
	Ctx context.Context
	// Policy is the run's tool policy — the same one the loop would have
	// enforced. It decides both what tools/list advertises and what tools/call
	// will execute.
	Policy domain.ToolPolicy
	// TaskKey identifies the run in this endpoint's logs. Not used for
	// authorization.
	TaskKey string
	// SkillsOnDisk says this run's skills are files in its workspace, so
	// load_skill is withheld from it. See claudecode.MCPRun.SkillsOnDisk for
	// what goes wrong when it is set the other way.
	SkillsOnDisk bool
	// ExpiresAt is an absolute ceiling on the credential, independent of the
	// run.
	//
	// Zero means "no ceiling, the run's own lifetime is the token's" — which is
	// what a loopback token gets, because a token that never leaves the host it
	// was minted on cannot outlive the process that holds the only copy of it.
	// A token handed to a MACHINE ON THE INTERNET is a different object: it is
	// written to a file in somebody's home directory, and the only thing this
	// process can still promise about it is that it stops working. So the
	// remote provider always sets one, and Lookup enforces it.
	//
	// It is a BACKSTOP, not the revocation. The revocation is the executor's
	// defer, which fires however the run ended; the ceiling is what covers a
	// copy that outlived it — and it is set past the run's own timeout for the
	// reason on runtime.mcpTokenGrace, so it never expires a session that is
	// still working.
	ExpiresAt time.Time
}

// live reports whether this token is still usable.
//
// Two ways to be dead: revoked (handled by the caller — the entry is simply
// gone) and expired. Every way a run can END goes through the first, because
// the executor revokes from a defer: a finished session, a failed one, a
// cancelled one, a dropped tunnel and a timed-out call all return from Execute
// and all run that defer.
//
// What is deliberately NOT a third way is Ctx being cancelled, and the reason
// is on TestTokenSurvivesRunContextCancellation: while the child process is
// still alive, turning its next call into a 401 makes Claude Code report
// `requires re-authorization (token expired)` and stop using the server for the
// rest of the session. A cancelled run's tool call has to fail as a cancelled
// call, with the reason — which it does, because the cancellation rides on the
// context the call executes under — not as an authentication problem.
func (r Run) live(now time.Time) bool {
	if r.Ctx == nil {
		return false
	}
	return r.ExpiresAt.IsZero() || now.Before(r.ExpiresAt)
}

// tokenBytes is the size of a run token before base64url. 32 bytes is 256 bits
// of crypto/rand: the token is the ONLY thing standing between a caller and
// the board tools, and it travels in a file rather than a command
// line precisely because it is a credential.
const tokenBytes = 32

// RunTokenRegistry maps live per-run bearer tokens to the runs they belong to.
//
// In memory on purpose. A token dies with the run that minted it — the executor
// revokes it on every exit path — so there is nothing to persist: a run that was
// parked and later resumed is a NEW run row and mints a fresh token, and a
// process restart leaves no session that could still be holding an old one.
//
// A restart is in fact the strongest revocation this design has, and it is why
// the map is not backed by a table even now that a token is carried over the
// public internet: the credential a laptop is holding stops working the instant
// this process goes away, whether it went away cleanly or not.
type RunTokenRegistry struct {
	mu   sync.RWMutex
	runs map[string]Run
	// now is time.Now, overridden by the expiry tests.
	now func() time.Time
}

func NewRunTokenRegistry() *RunTokenRegistry {
	return &RunTokenRegistry{runs: make(map[string]Run), now: time.Now}
}

func (r *RunTokenRegistry) clock() time.Time {
	if r.now == nil {
		return time.Now()
	}
	return r.now()
}

// Mint issues a token for one run. The caller must Revoke it when the run ends.
func (r *RunTokenRegistry) Mint(run Run) (string, error) {
	if run.Ctx == nil {
		// Without the run's context there is no attribution, and every tool call
		// would land on a background context with no workspace, no task and no
		// ledger. Refuse rather than serve an unattributable session.
		return "", errors.New("mcpserver: a run token needs the run's context")
	}
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("mint mcp run token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buf)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs[token] = run
	return token, nil
}

// Revoke drops a token. Idempotent, so the executor can call it from a defer
// without caring which exit path it is on.
func (r *RunTokenRegistry) Revoke(token string) {
	if token == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.runs, token)
}

// Lookup resolves a presented token to a run that is still live.
//
// A map lookup rather than a constant-time compare over the live tokens, unlike
// the API keys in adapter/http: those are long-lived operator-chosen secrets
// compared one by one, while this is 256 bits of crypto/rand hashed in one step.
// There is no prefix to walk and nothing an attacker could learn from the
// timing that guessing 2^256 values would not already have cost them.
//
// The liveness check is here rather than at the call sites for the reason the
// tool surface is computed in one place: a second copy of "is this run still
// running" would eventually disagree with the first, and the disagreement would
// be a session acting for a run that finished.
func (r *RunTokenRegistry) Lookup(token string) (Run, bool) {
	if token == "" {
		return Run{}, false
	}
	r.mu.RLock()
	run, ok := r.runs[token]
	r.mu.RUnlock()
	if !ok {
		return Run{}, false
	}
	if !run.live(r.clock()) {
		// Dropped as well as refused, so a token whose run ended without its
		// executor getting to the revoke — the process was killed mid-defer,
		// the tunnel died — does not sit in the map until restart.
		r.Revoke(token)
		return Run{}, false
	}
	return run, true
}

// Live is how many runs currently hold a token. For tests and for a boot-time
// sanity log; nothing in the request path reads it.
func (r *RunTokenRegistry) Live() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.runs)
}
