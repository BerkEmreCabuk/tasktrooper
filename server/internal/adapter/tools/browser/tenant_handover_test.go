package browser

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

// The shared browser must not carry one tenant's session into another's call.
//
// One chromium, one profile, one tab, for the whole process: correct while a
// process served one tenant, and a cross-tenant session leak now. A QA agent
// that logs into its tenant's staging app leaves those cookies and that open
// page behind; the next tenant's agent would arrive authenticated as somebody
// else and browser_read_dom would hand the result to a model.
//
// These tests drive handoverLocked directly rather than through a real
// chromium: the property under test is "the live browser is discarded when the
// tenant changes", and a test that needed the browser binary would not run
// anywhere this repository is built.

func ctxFor(t *testing.T, id uuid.UUID) context.Context {
	t.Helper()
	return tenant.With(context.Background(), tenant.Identity{TenantID: id, Role: tenant.RoleOwner})
}

// liveSession fakes a started browser: handoverLocked only looks at tabCtx to
// decide whether there is anything to tear down, and at the cancel funcs to do
// it.
func liveSession(t *testing.T, owner uuid.UUID) (*Session, *bool) {
	t.Helper()
	torn := false
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := NewSession()
	s.tenant = owner
	s.tabCtx = ctx
	s.tabCancel = func() { torn = true }
	s.allocCancel = func() {}
	return s, &torn
}

func TestBrowserIsTornDownWhenTheTenantChanges(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	s, torn := liveSession(t, a)

	s.handoverLocked(ctxFor(t, b))

	if !*torn {
		t.Fatal("tenant B must not inherit the browser tenant A was using")
	}
	if s.tabCtx != nil {
		t.Fatal("the torn-down browser must be forgotten, so the next call starts a fresh one")
	}
	if s.tenant != b {
		t.Fatalf("session owner = %s, want %s", s.tenant, b)
	}
}

func TestBrowserIsKeptForTheSameTenant(t *testing.T) {
	a := uuid.New()
	s, torn := liveSession(t, a)

	s.handoverLocked(ctxFor(t, a))

	if *torn {
		t.Fatal("a tenant's own next call must reuse its browser — navigate, click and screenshot share one page")
	}
	if s.tabCtx == nil {
		t.Fatal("the browser must still be live for the same tenant")
	}
}

// Self-hosted and desktop have one tenant and often no identity on a
// background context. They must keep exactly one browser for the life of the
// process, as they always did — the zero uuid is a stable key, not a bypass.
func TestSingleTenantKeepsOneBrowser(t *testing.T) {
	s, torn := liveSession(t, uuid.Nil)

	for i := 0; i < 3; i++ {
		s.handoverLocked(context.Background())
	}

	if *torn {
		t.Fatal("a deployment with no tenant on its context must not restart chromium on every call")
	}
}

// The first call on a cold session has nothing to tear down, and must not log
// or act as though it did.
func TestFirstCallOnAColdSessionTearsNothingDown(t *testing.T) {
	s := NewSession()
	id := uuid.New()

	s.handoverLocked(ctxFor(t, id))

	if s.tenant != id {
		t.Fatalf("session owner = %s, want %s", s.tenant, id)
	}
	if s.tabCtx != nil {
		t.Fatal("a cold session must stay cold until ensureLocked starts a browser")
	}
}

// The wiring, which is the part that actually protects anybody.
//
// The tests above drive handoverLocked directly, so they pass just as well
// against a run() that never calls it — which is exactly the mistake worth
// catching, because handoverLocked is only a safeguard if the single funnel
// every browser_* tool goes through invokes it. run does the handover BEFORE
// ensureLocked, so this works without a chromium binary: the call fails to
// start a browser afterwards, and the assertion is about what happened first.
func TestRunHandsOverBeforeDoingAnythingElse(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	s, torn := liveSession(t, a)

	// The error is expected and irrelevant: there is no chromium here, so
	// ensureLocked fails right after the handover.
	_ = s.run(ctxFor(t, b), guardTimeout)

	if !*torn {
		t.Fatal("run must hand the browser over when the tenant changes; " +
			"without it every browser_* tool inherits the previous tenant's session")
	}
	if s.tenant != b {
		t.Fatalf("session owner after run = %s, want %s", s.tenant, b)
	}
}
