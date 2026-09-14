package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const readDOMToolName = "browser_read_dom"

// readDOMMaxBytes caps what one call may put into the context; full page HTML
// on a real product easily runs to megabytes.
const readDOMMaxBytes = 100 * 1024

const (
	// readDOMSettleWindow is how long a selector that matches nothing is given
	// before the call gives up. It used to be the full 30s execute timeout,
	// because chromedp.Text waits for its node: a developer checking whether the
	// button it just added rendered paid half a minute per attempt and got
	// "context deadline exceeded" — a message about a browser, not about a page.
	// A few seconds covers the render a click or a navigation kicked off;
	// anything slower is what browser_wait_for is for.
	readDOMSettleWindow = 3 * time.Second
	readDOMSettlePoll   = 300 * time.Millisecond

	// readDOMMaxAnchors bounds the "what IS on this page" list that comes back
	// when the selector matched nothing.
	readDOMMaxAnchors = 30
	// readDOMMaxHits bounds the reported matches of a contains search.
	readDOMMaxHits = 10
)

type readDOMArgs struct {
	Selector string `json:"selector"`
	// AsText is a pointer because its default is true and encoding/json cannot
	// distinguish an absent field from an explicit false on a plain bool.
	AsText   *bool  `json:"as_text"`
	Contains string `json:"contains"`
}

type readDOMTool struct {
	session *Session
}

func newReadDOMTool(session *Session) port.ToolExecutor {
	return &readDOMTool{session: session}
}

func (t *readDOMTool) Name() string {
	return readDOMToolName
}

func (t *readDOMTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: readDOMToolName,
			Description: "Read the current page of the shared browser. Returns the page URL and title, then either the rendered text (default), the outer HTML (as_text:false), or — with contains — every element whose text or attributes hold a string, each with its selector and whether it is actually visible. " +
				"Use contains to answer \"did the thing I added render?\": rendered text does not include hidden elements or icon-only buttons, so an empty text read is not proof of absence. Output is truncated at 100KB.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"selector": map[string]interface{}{
						"type":        "string",
						"description": "CSS selector of the element to read (default: body)",
					},
					"as_text": map[string]interface{}{
						"type":        "boolean",
						"description": "true returns rendered innerText, false returns outer HTML (default: true)",
					},
					"contains": map[string]interface{}{
						"type":        "string",
						"description": "Search the element's subtree for this string in text and attributes instead of dumping it. Returns each match with its selector, visibility and surrounding HTML — the reliable way to check whether something is on the page.",
					},
				},
			},
		},
	}
}

// domHit is one element a contains search matched.
type domHit struct {
	Selector string `json:"selector"`
	Text     string `json:"text"`
	HTML     string `json:"html"`
	Visible  bool   `json:"visible"`
	Where    string `json:"where"`
}

// domProbe is what one evaluation of readDOMScript reports back. Everything the
// tool answers with comes from this single round-trip: where the tab actually
// is, whether the selector matched, and only then the content.
type domProbe struct {
	URL         string   `json:"url"`
	Title       string   `json:"title"`
	Matches     int      `json:"matches"`
	BadSelector string   `json:"bad_selector"`
	Text        string   `json:"text"`
	TextLen     int      `json:"text_len"`
	HTML        string   `json:"html"`
	HTMLLen     int      `json:"html_len"`
	Visible     bool     `json:"visible"`
	Anchors     []string `json:"anchors"`
	Hits        []domHit `json:"hits"`
	HitCount    int      `json:"hit_count"`
}

// readDOMScript runs entirely inside the page so that one CDP round-trip
// answers all three questions a read can have: is the tab where the agent
// thinks it is, did the selector match, and what does the match hold. It never
// waits for a node — the polling that replaces chromedp's built-in wait lives in
// Go, where it can be bounded at seconds instead of at the execute timeout.
var readDOMScript = `(function(sel, needle, wantText, maxChars) {` + domJSHelpers + `
  var out = {url: location.href, title: document.title, matches: 0};
  var nodes;
  try { nodes = document.querySelectorAll(sel); }
  catch (e) { out.bad_selector = String((e && e.message) || e); return JSON.stringify(out); }
  out.matches = nodes.length;
  if (!nodes.length) {
    out.anchors = anchorList(` + fmt.Sprint(readDOMMaxAnchors) + `);
    return JSON.stringify(out);
  }
  var el = nodes[0];
  if (needle) {
    var low = String(needle).toLowerCase();
    var hits = [], total = 0;
    var scope = [el].concat(Array.prototype.slice.call(el.querySelectorAll('*')));
    for (var j = 0; j < scope.length; j++) {
      var n = scope[j];
      var inText = (n.textContent || '').toLowerCase().indexOf(low) >= 0;
      var deeper = false;
      if (inText) {
        for (var k = 0; k < n.children.length; k++) {
          if ((n.children[k].textContent || '').toLowerCase().indexOf(low) >= 0) { deeper = true; break; }
        }
      }
      var inAttr = false;
      if (!inText && n.attributes) {
        for (var a = 0; a < n.attributes.length; a++) {
          if (String(n.attributes[a].value).toLowerCase().indexOf(low) >= 0) { inAttr = true; break; }
        }
      }
      if ((inText && !deeper) || inAttr) {
        total++;
        if (hits.length < ` + fmt.Sprint(readDOMMaxHits) + `) {
          hits.push({
            selector: path(n),
            text: (n.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 160),
            html: (n.outerHTML || '').slice(0, 400),
            visible: vis(n),
            where: inText ? 'text' : 'attributes'
          });
        }
      }
    }
    out.hits = hits;
    out.hit_count = total;
    return JSON.stringify(out);
  }
  var text = el.innerText || '';
  var html = el.outerHTML || '';
  out.text_len = text.length;
  out.html_len = html.length;
  out.visible = vis(el);
  if (wantText) { out.text = text.slice(0, maxChars); } else { out.html = html.slice(0, maxChars); }
  return JSON.stringify(out);
})(%s, %s, %s, %d)`

func (t *readDOMTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var a readDOMArgs
	if err := json.Unmarshal([]byte(arguments), &a); err != nil {
		return toolError(readDOMToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	selector := strings.TrimSpace(a.Selector)
	if selector == "" {
		selector = "body"
	}
	asText := a.AsText == nil || *a.AsText

	probe, err := t.probe(ctx, selector, a.Contains, asText)
	if err != nil {
		return runError(readDOMToolName, fmt.Sprintf("read %q", selector), err)
	}

	if probe.BadSelector != "" {
		return toolError(readDOMToolName, fmt.Sprintf("%q is not a valid CSS selector: %s", selector, probe.BadSelector))
	}
	if probe.Matches == 0 {
		return toolError(readDOMToolName, notFoundReport(selector, probe))
	}
	return domain.ToolResult{
		Name:    readDOMToolName,
		Content: truncate(readReport(selector, a.Contains, asText, probe), readDOMMaxBytes),
		IsError: false,
	}
}

// probe evaluates readDOMScript, re-running it for a few seconds while the
// selector matches nothing. The retry is the whole reason a match is polled in
// Go: a page that is still rendering must still be read, but a selector that is
// simply not there must come back as a fact about the page rather than as a
// timeout the model reads as a broken browser.
func (t *readDOMTool) probe(ctx context.Context, selector, contains string, asText bool) (domProbe, error) {
	script := fmt.Sprintf(readDOMScript,
		mustJSON(selector), mustJSON(contains), mustJSON(asText), readDOMMaxBytes)

	var probe domProbe
	action := chromedp.ActionFunc(func(runCtx context.Context) error {
		deadline := time.Now().Add(readDOMSettleWindow)
		for {
			var raw string
			if err := chromedp.Evaluate(script, &raw).Do(runCtx); err != nil {
				return err
			}
			var next domProbe
			if err := json.Unmarshal([]byte(raw), &next); err != nil {
				return fmt.Errorf("decode page probe: %w", err)
			}
			probe = next
			if next.Matches > 0 || next.BadSelector != "" || time.Now().After(deadline) {
				return nil
			}
			select {
			case <-runCtx.Done():
				return runCtx.Err()
			case <-time.After(readDOMSettlePoll):
			}
		}
	})

	if err := t.session.run(ctx, executeTimeout, action); err != nil {
		return domProbe{}, err
	}
	return probe, nil
}

// pageHeader is the line every read answers with, whatever it found. Without it
// a read is a bare blob of text: an agent that had been scrubbed onto
// about:blank, or that never left the login page, read "" and concluded its own
// change had not rendered.
func pageHeader(probe domProbe) string {
	title := probe.Title
	if title == "" {
		title = "(no title)"
	}
	return fmt.Sprintf("url: %s\ntitle: %s", probe.URL, title)
}

// notFoundReport answers a selector that matched nothing with what the page
// does hold, so the next call can be a better selector instead of the same one.
func notFoundReport(selector string, probe domProbe) string {
	var b strings.Builder
	fmt.Fprintf(&b, "no element matches %q after %s.\n%s\n", selector, readDOMSettleWindow, pageHeader(probe))
	if len(probe.Anchors) == 0 {
		b.WriteString("\nThe page has no elements with an id, no buttons, links or inputs at all — it is most likely blank or still loading. Check the url above is the one you meant.")
		return b.String()
	}
	b.WriteString("\nElements that ARE on this page:\n")
	for _, anchor := range probe.Anchors {
		fmt.Fprintf(&b, "- %s\n", anchor)
	}
	b.WriteString("\nPick one of these, or call browser_wait_for if the element renders later. Repeating this exact call will return this same answer.")
	return b.String()
}

// readReport renders a successful read: the page, the match, and the content —
// and, where the content is empty, why it is empty. An empty answer used to be
// an empty tool result, which reads as a failure and is the reason a run can
// spend fifty turns asking the same question.
func readReport(selector, contains string, asText bool, probe domProbe) string {
	var b strings.Builder
	b.WriteString(pageHeader(probe))
	fmt.Fprintf(&b, "\nselector: %s — %d match(es)\n", selector, probe.Matches)

	if contains != "" {
		if probe.HitCount == 0 {
			fmt.Fprintf(&b, "\n%q appears nowhere in the text or attributes of %s on this page. It is not rendered here — this is a definitive answer, do not re-read the DOM to confirm it. If you expected it, the page is stale (reload), the build did not include your change, or the element is on another route.", contains, selector)
			return b.String()
		}
		fmt.Fprintf(&b, "\n%q found in %d element(s)", contains, probe.HitCount)
		if probe.HitCount > len(probe.Hits) {
			fmt.Fprintf(&b, " (first %d shown)", len(probe.Hits))
		}
		b.WriteString(":\n")
		for i, hit := range probe.Hits {
			state := "visible"
			if !hit.Visible {
				state = "NOT visible (hidden, zero-size or transparent)"
			}
			fmt.Fprintf(&b, "\n%d. %s — %s — matched in %s\n   text: %s\n   html: %s\n",
				i+1, hit.Selector, state, hit.Where, hit.Text, hit.HTML)
		}
		return b.String()
	}

	// Rendered text falls back to the DOM's text for an element that is not
	// being rendered, so a read can return the label of a button no user can
	// see. That difference is the whole question a UI check is asking, so it is
	// said before the content rather than left to be inferred from it.
	if !probe.Visible {
		b.WriteString("note: this element is in the DOM but NOT visible (hidden, zero-size or transparent). " +
			"What follows is what the page holds, not what a user sees.\n")
	}

	if asText {
		if strings.TrimSpace(probe.Text) == "" {
			fmt.Fprintf(&b, "\nThe element matched but its rendered text is empty (outer HTML is %d chars, element %s). "+
				"Rendered text never includes hidden elements, icon-only buttons or attribute values — an empty read is NOT proof the content is missing. "+
				"Call again with as_text:false to read the HTML, or with contains:\"<what you are looking for>\" to search text and attributes.",
				probe.HTMLLen, visibility(probe.Visible))
			return b.String()
		}
		fmt.Fprintf(&b, "text (%d chars):\n%s", probe.TextLen, probe.Text)
		return b.String()
	}

	if strings.TrimSpace(probe.HTML) == "" {
		b.WriteString("\nThe element matched but has no outer HTML, which should not happen — re-read with a different selector.")
		return b.String()
	}
	fmt.Fprintf(&b, "html (%d chars", probe.HTMLLen)
	if probe.HTMLLen > len(probe.HTML) {
		fmt.Fprintf(&b, ", first %d shown — use contains to search the rest instead of paging through it", len(probe.HTML))
	}
	fmt.Fprintf(&b, "):\n%s", probe.HTML)
	return b.String()
}

func visibility(visible bool) string {
	if visible {
		return "is visible"
	}
	return "is NOT visible"
}

// mustJSON renders a Go value as a JavaScript literal for the script above.
// Marshalling a string or a bool cannot fail; a failure would still be a bug
// worth seeing rather than a silent empty argument, so it becomes "null".
func mustJSON(v interface{}) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(raw)
}
