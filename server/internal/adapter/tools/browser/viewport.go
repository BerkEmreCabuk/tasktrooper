package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/device"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

const viewportToolName = "browser_set_viewport"

// maxOverflowItems bounds the reported horizontal overflow. Fifteen elements is
// already a layout that is broken in one place and inherited everywhere below
// it; a longer list is the same finding repeated.
const maxOverflowItems = 15

type viewportArgs struct {
	Device string `json:"device"`
	Width  int64  `json:"width"`
	Height int64  `json:"height"`
}

type viewportTool struct {
	session *Session
}

func newViewportTool(session *Session) port.ToolExecutor {
	return &viewportTool{session: session}
}

func (t *viewportTool) Name() string {
	return viewportToolName
}

func (t *viewportTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Type: "function",
		Function: domain.FunctionDefinition{
			Name: viewportToolName,
			Description: "Switch the shared browser between mobile, tablet and desktop emulation — viewport size, device pixel ratio, touch support and mobile user agent — and report the responsive state of the current page: whether it scrolls horizontally and which elements overflow the viewport. " +
				"The emulation persists for every later browser_* call, so read the DOM and take screenshots after switching. Call with no arguments to report the current viewport without changing it.",
			Parameters: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"device": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"mobile", "tablet", "desktop"},
						"description": "Preset to emulate: mobile (390x844, touch, mobile user agent), tablet (768x1024, touch) or desktop (1440x900, no touch)",
					},
					"width": map[string]interface{}{
						"type":        "integer",
						"description": "Custom viewport width in CSS pixels; overrides the preset width",
					},
					"height": map[string]interface{}{
						"type":        "integer",
						"description": "Custom viewport height in CSS pixels; overrides the preset height",
					},
				},
			},
		},
	}
}

// overflowItem is one element sticking out past the right edge of the viewport.
type overflowItem struct {
	Selector string `json:"selector"`
	Left     int    `json:"left"`
	Right    int    `json:"right"`
	Width    int    `json:"width"`
}

type responsiveReport struct {
	URL           string         `json:"url"`
	Title         string         `json:"title"`
	Width         int            `json:"width"`
	Height        int            `json:"height"`
	DocWidth      int            `json:"doc_width"`
	DPR           float64        `json:"dpr"`
	Touch         bool           `json:"touch"`
	Mobile        bool           `json:"mobile_ua"`
	CoarsePointer bool           `json:"coarse_pointer"`
	NarrowMedia   bool           `json:"narrow_media"`
	ScrollsX      bool           `json:"scrolls_x"`
	Overflow      []overflowItem `json:"overflow"`
	OverflowCount int            `json:"overflow_count"`
}

// responsiveScript measures what a responsive check actually asks: did the CSS
// switch to its narrow branch, and does anything stick out past the edge.
//
// An element that overflows inside a container that clips or scrolls is not a
// layout bug — carousels, drawers and sticky menus all park content off-screen
// on purpose — so those are skipped rather than reported as breakage the agent
// would then "fix".
var responsiveScript = `(function(maxItems) {` + domJSHelpers + `
  var de = document.documentElement;
  var vw = de.clientWidth, vh = de.clientHeight;
  var body = document.body;
  var out = {
    url: location.href, title: document.title,
    width: vw, height: vh,
    doc_width: Math.max(de.scrollWidth, body ? body.scrollWidth : 0),
    dpr: window.devicePixelRatio,
    touch: ('ontouchstart' in window) || navigator.maxTouchPoints > 0,
    mobile_ua: /Mobile|Android|iPhone|iPad/i.test(navigator.userAgent),
    coarse_pointer: window.matchMedia('(pointer: coarse)').matches,
    narrow_media: window.matchMedia('(max-width: 768px)').matches
  };
  out.scrolls_x = out.doc_width > vw + 1;
  function clipped(n) {
    var p = n.parentElement, depth = 0;
    while (p && depth < 8) {
      var st = getComputedStyle(p);
      if (st.overflowX !== 'visible' || st.overflow !== 'visible') return true;
      p = p.parentElement; depth++;
    }
    return false;
  }
  var over = [], total = 0;
  var all = body ? body.querySelectorAll('*') : [];
  for (var i = 0; i < all.length; i++) {
    var n = all[i];
    var r = n.getBoundingClientRect();
    if (r.right <= vw + 1 && r.left >= -1) continue;
    if (r.width < 4 && r.height < 4) continue;
    if (!vis(n)) continue;
    if (clipped(n)) continue;
    total++;
    if (over.length < maxItems) {
      over.push({selector: path(n), left: Math.round(r.left), right: Math.round(r.right), width: Math.round(r.width)});
    }
  }
  out.overflow = over;
  out.overflow_count = total;
  return JSON.stringify(out);
})(%d)`

func (t *viewportTool) Execute(ctx context.Context, arguments string) domain.ToolResult {
	var a viewportArgs
	if err := json.Unmarshal([]byte(arguments), &a); err != nil {
		return toolError(viewportToolName, fmt.Sprintf("invalid arguments: %v", err))
	}
	preset := strings.ToLower(strings.TrimSpace(a.Device))
	switch preset {
	case "", "mobile", "tablet", "desktop":
	default:
		return toolError(viewportToolName, fmt.Sprintf("unknown device %q: use mobile, tablet or desktop", a.Device))
	}
	if a.Width < 0 || a.Height < 0 {
		return toolError(viewportToolName, "width and height must be positive")
	}

	actions := emulationActions(preset, a.Width, a.Height)

	var raw string
	actions = append(actions, chromedp.Evaluate(fmt.Sprintf(responsiveScript, maxOverflowItems), &raw))
	if err := t.session.run(ctx, executeTimeout, actions...); err != nil {
		return runError(viewportToolName, "set viewport", err)
	}

	var report responsiveReport
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return toolError(viewportToolName, fmt.Sprintf("decode responsive report: %v", err))
	}
	return domain.ToolResult{
		Name:    viewportToolName,
		Content: renderResponsiveReport(preset, report),
		IsError: false,
	}
}

// emulationActions turns the requested preset into CDP emulation. A preset with
// a custom size still keeps its touch and user-agent emulation: "mobile at
// 320px" must stay a phone, or the check is a narrow desktop window instead.
func emulationActions(preset string, width, height int64) []chromedp.Action {
	var actions []chromedp.Action
	switch preset {
	case "mobile":
		actions = append(actions, chromedp.Emulate(device.IPhone13))
	case "tablet":
		actions = append(actions, chromedp.Emulate(device.IPad))
	case "desktop":
		actions = append(actions, chromedp.Emulate(device.Reset),
			chromedp.EmulateViewport(defaultViewportWidth, defaultViewportHeight))
	}
	if width > 0 || height > 0 {
		w, h := width, height
		if w <= 0 {
			w = defaultViewportWidth
		}
		if h <= 0 {
			h = defaultViewportHeight
		}
		actions = append(actions, chromedp.EmulateViewport(w, h))
	}
	return actions
}

func renderResponsiveReport(preset string, r responsiveReport) string {
	var b strings.Builder
	if preset != "" {
		fmt.Fprintf(&b, "emulating: %s\n", preset)
	}
	title := r.Title
	if title == "" {
		title = "(no title)"
	}
	fmt.Fprintf(&b, "url: %s\ntitle: %s\n", r.URL, title)
	fmt.Fprintf(&b, "viewport: %dx%d css px, dpr %g, touch %v, mobile user agent %v\n", r.Width, r.Height, r.DPR, r.Touch, r.Mobile)
	fmt.Fprintf(&b, "css state: (max-width: 768px) %v, (pointer: coarse) %v\n", r.NarrowMedia, r.CoarsePointer)

	if !r.ScrollsX && r.OverflowCount == 0 {
		b.WriteString("\nlayout: no horizontal scrolling and nothing overflows the viewport at this size.")
		return b.String()
	}
	if r.ScrollsX {
		fmt.Fprintf(&b, "\nlayout: the page scrolls horizontally — content is %d px wide in a %d px viewport.\n", r.DocWidth, r.Width)
	} else {
		b.WriteString("\nlayout: the page itself does not scroll horizontally, but content sticks out past the edge.\n")
	}
	if r.OverflowCount == 0 {
		b.WriteString("No single element could be pinned down as the cause — a fixed width or a min-width on a container is the usual reason.")
		return b.String()
	}
	fmt.Fprintf(&b, "\n%d element(s) extend past the viewport", r.OverflowCount)
	if r.OverflowCount > len(r.Overflow) {
		fmt.Fprintf(&b, " (first %d shown)", len(r.Overflow))
	}
	b.WriteString(":\n")
	for _, item := range r.Overflow {
		fmt.Fprintf(&b, "- %s — spans x %d…%d (%d px wide)\n", item.Selector, item.Left, item.Right, item.Width)
	}
	b.WriteString("\nElements inside a clipping or scrolling container are excluded, so these are real overflow. Take a screenshot to see them.")
	return b.String()
}
