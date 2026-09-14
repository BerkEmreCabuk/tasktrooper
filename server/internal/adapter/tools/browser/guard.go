package browser

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

// errBlockedPage is what every destination-level refusal inside this package
// collapses to. It exists so the tools can tell "the guard refused this" from
// "the selector was not there" internally, while saying the same thing to the
// model in the first case as they do for a refused connection.
var errBlockedPage = errors.New("browser: destination not permitted")

// navFailedMsg is the only thing browser_navigate ever says about a failure
// other than a missing chromium binary, and pageUnavailableMsg is the same idea
// for the tools that act on whatever page the tab is already showing.
//
// The distinction that used to leak was not cosmetic. "refusing link-local
// address 169.254.169.254", "net::ERR_CONNECTION_REFUSED" and a timeout are
// three different answers to "is something listening there", the model picks
// the URL, and the answer lands right back in its context — that is a working
// internal port scanner with the LLM as its operator. Blocked, refused, reset,
// unresolvable and timed out are now one message; the reason goes to the log.
const (
	navFailedMsg       = "could not open that URL"
	pageUnavailableMsg = "could not use the current page"
)

// maxCheckedFrameURLs bounds how many distinct frame URLs the choke point vets
// in one pass, so a page cannot turn the guard into a DNS amplifier by embedding
// a thousand iframes. Going over the cap is treated as a refusal rather than as
// a reason to stop checking: skipping the tail is exactly how an attacker would
// hide the one frame that matters.
const maxCheckedFrameURLs = 64

// defaultPolicy is the destination policy every browser tool is judged against.
//
// # Loopback is on here, and that is a deliberate departure
//
// fetch_url, the MCP client and the job callbacks all take urlguard.Default,
// where loopback is off unless the operator sets ALLOW_LOOPBACK_TOOL_URLS. This
// package inverts that default, on purpose:
//
//   - It is what the tool is for. The QA agent's documented flow (catalog
//     seeddata/prompts/qa-agent.md step 3) is "boot the task branch locally in
//     the task workspace ... build and serve the app, drive the changed flows
//     with the browser tools". The app under test is a dev server the agent
//     just started on 127.0.0.1:PORT in its own workspace, so http://localhost:
//     8080/login is the single most common legitimate destination this tool has.
//     browser_test.go has asserted it passes since the tool was written.
//   - Refusing it would not buy a boundary. The same agent, in the same pod,
//     holds run_terminal (config.yml tools.terminal), whose blocklist stops
//     rm -rf and sudo, not curl. An agent that wants this pod's own listener has
//     a shell on it. A browser-side loopback ban would remove the product
//     feature and leave the capability untouched.
//
// What loopback does NOT drag in with it is the part that matters, and it is
// the reason this is AllowLoopback rather than AllowPrivate or a hand-rolled
// check: 169.254.169.254 and the rest of link-local, RFC1918, IPv6 ULA, CGNAT,
// multicast, the unspecified address and every alternate encoding of those stay
// refused. Those are the other tenants' pods, the database, the VPC and the
// cloud metadata service — things on the far side of a boundary the NetworkPolicy
// is supposed to hold but that is applied and not positively verified. Loopback
// is the one range that same NetworkPolicy structurally cannot see, and it is
// also the only one this tool needs.
//
// An operator who does not want it can still close it: ALLOW_LOOPBACK_TOOL_URLS
// set to a false value turns it off here too. Unset means on, because the
// self-hosted and cloud QA flows both depend on it.
func defaultPolicy() urlguard.Policy {
	p := urlguard.PublicOnly()
	p.AllowLoopback = loopbackAllowed(os.Getenv)
	return p
}

// loopbackAllowed reads the same operator switch urlguard.Default reads, with
// the sense the browser tool needs: absent means allowed. getenv is a parameter
// so the switch is testable without mutating the process environment.
func loopbackAllowed(getenv func(string) string) bool {
	raw := strings.TrimSpace(getenv(urlguard.AllowLoopbackEnv))
	if raw == "" {
		return true
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		// A typo must not silently disable the QA agent's only environment.
		return true
	}
	return v
}

// inertSchemes are the schemes a frame can carry that fetch nothing over the
// network, so there is no destination to judge. about:blank is what the guard
// itself navigates to; about:srcdoc is content the parent document already
// supplied and that was vetted with the parent; chrome-error is chromium's own
// failure page; data: and blob: are bytes that never left the browser (a PDF
// preview or a sandboxed editor frame is routinely one of these, and refusing
// them would break real pages for no gain).
//
// Everything outside this set — including file:, view-source: and chrome: — is
// handed to the policy, which allows http and https only. That is deliberate:
// file:///etc/passwd rendered in the tab is a browser_read_dom away from the
// model, and view-source:http://169.254.169.254/ fetches exactly what the plain
// http URL would.
var inertSchemes = map[string]struct{}{
	"about":            {},
	"data":             {},
	"blob":             {},
	"chrome-error":     {},
	"chrome-extension": {}, // packaged bytes, and the tools image ships no extensions
}

// pageURLAllowed judges one URL read off the live tab. The returned error is
// log-only and names the reason; callers hand the model errBlockedPage instead.
func pageURLAllowed(ctx context.Context, p urlguard.Policy, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("page URL does not parse: %w", err)
	}
	if _, inert := inertSchemes[strings.ToLower(u.Scheme)]; inert {
		return nil
	}
	// Full guard, not Precheck: the host here is whatever the page navigated to,
	// and a name is the whole point of the bypass — meta.attacker.example with an
	// A record pointing at 169.254.169.254 passes any check that only looks at
	// the spelling. Validate resolves and judges the answer.
	if _, err := p.Validate(ctx, raw); err != nil {
		return err
	}
	return nil
}
