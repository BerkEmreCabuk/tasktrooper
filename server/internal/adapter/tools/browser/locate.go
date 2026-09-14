package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// interactWaitWindow bounds how long browser_click and browser_fill wait for
// their element. It is deliberately far below executeTimeout: a failed click
// used to cost the full thirty seconds and then say "context deadline
// exceeded", so an agent hunting for a control it had just added paid half a
// minute per guess and learned nothing from it. Ten seconds covers the render a
// navigation or a previous click kicked off; anything slower is what
// browser_wait_for, with its own explicit timeout, is for.
const interactWaitWindow = 10 * time.Second

// domJSHelpers are shared by every script this package evaluates in the page.
// Function declarations hoist, so they may sit at the top of an IIFE and be
// used anywhere below.
const domJSHelpers = `
  function vis(n) {
    try {
      var r = n.getBoundingClientRect();
      if (!r.width && !r.height) return false;
      var st = getComputedStyle(n);
      return st.visibility !== 'hidden' && st.display !== 'none' && st.opacity !== '0';
    } catch (e) { return false; }
  }
  function path(n) {
    var parts = [];
    while (n && n.nodeType === 1 && parts.length < 6) {
      var p = n.tagName.toLowerCase();
      if (n.id) { parts.unshift(p + '#' + n.id); break; }
      var tid = n.getAttribute && n.getAttribute('data-testid');
      if (tid) { parts.unshift(p + '[data-testid="' + tid + '"]'); break; }
      if (n.classList && n.classList.length) {
        p += '.' + Array.prototype.slice.call(n.classList, 0, 2).join('.');
      }
      parts.unshift(p);
      n = n.parentElement;
    }
    return parts.join(' > ');
  }
  function anchorList(limit) {
    var out = [];
    var all = document.querySelectorAll('[id],[data-testid],button,a[href],input,[role="button"]');
    for (var i = 0; i < all.length && out.length < limit; i++) {
      out.push(path(all[i]) + (vis(all[i]) ? '' : ' (hidden)'));
    }
    return out;
  }
`

// locateScript answers the three questions a failed interaction raises, in one
// round-trip: where is the tab, does the selector match anything, and is any of
// what it matched something a user could see.
var locateScript = `(function(sel) {` + domJSHelpers + `
  var out = {url: location.href, title: document.title, matches: 0, visible: 0};
  var nodes;
  try { nodes = document.querySelectorAll(sel); }
  catch (e) { out.bad_selector = String((e && e.message) || e); return JSON.stringify(out); }
  out.matches = nodes.length;
  for (var i = 0; i < nodes.length; i++) { if (vis(nodes[i])) out.visible++; }
  if (!nodes.length) { out.anchors = anchorList(%d); }
  return JSON.stringify(out);
})(%s)`

// elementProbe is what the page said about one selector.
type elementProbe struct {
	URL         string   `json:"url"`
	Title       string   `json:"title"`
	Matches     int      `json:"matches"`
	Visible     int      `json:"visible"`
	BadSelector string   `json:"bad_selector"`
	Anchors     []string `json:"anchors"`
}

func (p elementProbe) header() string {
	title := p.Title
	if title == "" {
		title = "(no title)"
	}
	return fmt.Sprintf("url: %s\ntitle: %s", p.URL, title)
}

// locate evaluates locateScript against the current tab. It goes through
// Session.run like every other browser action, so the destination guard applies
// to it as well: a diagnosis is still a read of the page.
func (s *Session) locate(ctx context.Context, selector string) (elementProbe, error) {
	script := fmt.Sprintf(locateScript, readDOMMaxAnchors, mustJSON(selector))
	var raw string
	if err := s.run(ctx, guardTimeout, chromedp.Evaluate(script, &raw)); err != nil {
		return elementProbe{}, err
	}
	var probe elementProbe
	if err := json.Unmarshal([]byte(raw), &probe); err != nil {
		return elementProbe{}, fmt.Errorf("decode page probe: %w", err)
	}
	return probe, nil
}

// explainSelectorFailure turns a failed click, fill or wait into the fact about
// the page that caused it.
//
// "context deadline exceeded" is true and useless: it does not say whether the
// element is absent, present but invisible, or present and visible but not
// interactable, and those three need three different next moves. The security
// split from runError is kept exactly — a refused destination, a refused
// connection and a missing chromium still say only what they said before, since
// that difference is the one the model must not be able to read.
func explainSelectorFailure(ctx context.Context, s *Session, name, op, selector string, err error) domain.ToolResult {
	if errors.Is(err, errChromeNotFound) || errors.Is(err, errBlockedPage) || isTransportError(err) {
		return runError(name, op, err)
	}
	probe, probeErr := s.locate(ctx, selector)
	if probeErr != nil {
		return runError(name, op, err)
	}

	var b strings.Builder
	switch {
	case probe.BadSelector != "":
		return toolError(name, fmt.Sprintf("%q is not a valid CSS selector: %s", selector, probe.BadSelector))
	case probe.Matches == 0:
		fmt.Fprintf(&b, "%s failed: no element matches %q on this page.\n%s\n", op, selector, probe.header())
		if len(probe.Anchors) == 0 {
			b.WriteString("\nThe page has no elements with an id, no buttons, links or inputs at all — it is blank or still loading. Check the url above is the one you meant.")
			break
		}
		b.WriteString("\nElements that ARE on this page:\n")
		for _, anchor := range probe.Anchors {
			fmt.Fprintf(&b, "- %s\n", anchor)
		}
		b.WriteString("\nUse one of these, or browser_read_dom with contains to search the page. Repeating this exact call will fail the same way.")
	case probe.Visible == 0:
		fmt.Fprintf(&b, "%s failed: %q matches %d element(s) in the DOM, but none of them is visible "+
			"(hidden, zero-size or transparent), so it cannot be interacted with.\n%s\n"+
			"\nThe markup is there and the rendering is not: open the container that holds it first, or fix the styling that hides it. "+
			"browser_read_dom with contains shows its HTML.",
			op, selector, probe.Matches, probe.header())
	default:
		fmt.Fprintf(&b, "%s failed although %q matches %d visible element(s).\n%s\n"+
			"\nThe element is on the page but the action did not complete — it may be covered by an overlay, disabled, or moving. "+
			"Take a screenshot to see the state of the page before trying again.",
			op, selector, probe.Visible, probe.header())
	}
	return toolError(name, b.String())
}
