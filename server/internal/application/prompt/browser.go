package prompt

// The browser tool package collapses every destination-guard refusal to one
// of three fixed sentences on purpose (see adapter/tools/browser/guard.go):
// the detail that would help a model port-scan the pod's network stays in the
// log, never in what reaches the model. These three keys are that boundary's
// wording, kept here so it renders the same way every time it is asked for.

var browserChromeNotFoundKey = Define[struct{}]("guard.browser_chrome_not_found", struct{}{})
var browserNavFailedKey = Define[struct{}]("guard.browser_nav_failed", struct{}{})
var browserPageUnavailableKey = Define[struct{}]("guard.browser_page_unavailable", struct{}{})

// BrowserChromeNotFoundText is what every browser_* tool says when this
// process has no chromium binary to drive.
func BrowserChromeNotFoundText() string { return Text(browserChromeNotFoundKey) }

// BrowserNavFailedText is what browser_navigate says about any destination
// the guard would not let it reach.
func BrowserNavFailedText() string { return Text(browserNavFailedKey) }

// BrowserPageUnavailableText is what every other browser_* tool says when the
// guard finds the tab already on a destination it would not have navigated
// to.
func BrowserPageUnavailableText() string { return Text(browserPageUnavailableKey) }

type browserBadSelectorInput struct{ Selector, Detail string }

var browserBadSelectorKey = Define("guard.browser_bad_selector", browserBadSelectorInput{Selector: `"["`, Detail: "'[' is not a valid selector"})

// BrowserBadSelectorText is shared by browser_click/fill/wait_for's
// diagnosis and browser_read_dom's own check when a selector the model gave
// does not parse as CSS. selector must already be %q-quoted.
func BrowserBadSelectorText(selector, detail string) string {
	return browserBadSelectorKey.Render(browserBadSelectorInput{Selector: selector, Detail: detail})
}

var browserLocateNoAnchorsKey = Define[struct{}]("guard.browser_locate_no_anchors", struct{}{})

// BrowserLocateNoAnchorsText is explainSelectorFailure's answer when a
// selector matched nothing and the page has no anchors to suggest either.
func BrowserLocateNoAnchorsText() string { return Text(browserLocateNoAnchorsKey) }

var browserLocatePickOneKey = Define[struct{}]("guard.browser_locate_pick_one", struct{}{})

// BrowserLocatePickOneText follows explainSelectorFailure's list of anchors
// actually on the page.
func BrowserLocatePickOneText() string { return Text(browserLocatePickOneKey) }

var browserLocateNotVisibleKey = Define[struct{}]("guard.browser_locate_not_visible", struct{}{})

// BrowserLocateNotVisibleText is explainSelectorFailure's answer when the
// selector matched but nothing it matched is visible.
func BrowserLocateNotVisibleText() string { return Text(browserLocateNotVisibleKey) }

var browserLocateActionIncompleteKey = Define[struct{}]("guard.browser_locate_action_incomplete", struct{}{})

// BrowserLocateActionIncompleteText is explainSelectorFailure's answer when
// the selector matched a visible element but the action still did not land.
func BrowserLocateActionIncompleteText() string { return Text(browserLocateActionIncompleteKey) }

var browserReadDOMNoAnchorsKey = Define[struct{}]("guard.browser_read_dom_no_anchors", struct{}{})

// BrowserReadDOMNoAnchorsText is notFoundReport's answer when a selector
// matched nothing and the page has no anchors to suggest either.
func BrowserReadDOMNoAnchorsText() string { return Text(browserReadDOMNoAnchorsKey) }

var browserReadDOMPickOneKey = Define[struct{}]("guard.browser_read_dom_pick_one", struct{}{})

// BrowserReadDOMPickOneText follows notFoundReport's list of anchors
// actually on the page.
func BrowserReadDOMPickOneText() string { return Text(browserReadDOMPickOneKey) }

type browserReadDOMContainsNotFoundInput struct{ Contains, Selector string }

var browserReadDOMContainsNotFoundKey = Define("tool_results.browser_read_dom_contains_not_found",
	browserReadDOMContainsNotFoundInput{Contains: `"widget"`, Selector: "body"})

// BrowserReadDOMContainsNotFoundText is browser_read_dom's answer when a
// contains search matched nothing. contains must already be %q-quoted.
func BrowserReadDOMContainsNotFoundText(contains, selector string) string {
	return browserReadDOMContainsNotFoundKey.Render(browserReadDOMContainsNotFoundInput{Contains: contains, Selector: selector})
}

type browserReadDOMEmptyTextInput struct {
	HTMLLen    int
	Visibility string
}

var browserReadDOMEmptyTextKey = Define("tool_results.browser_read_dom_empty_text",
	browserReadDOMEmptyTextInput{HTMLLen: 42, Visibility: "is visible"})

// BrowserReadDOMEmptyTextText is browser_read_dom's answer when as_text
// matched an element whose rendered text is empty.
func BrowserReadDOMEmptyTextText(htmlLen int, visibility string) string {
	return browserReadDOMEmptyTextKey.Render(browserReadDOMEmptyTextInput{HTMLLen: htmlLen, Visibility: visibility})
}

var browserReadDOMEmptyHTMLKey = Define[struct{}]("tool_results.browser_read_dom_empty_html", struct{}{})

// BrowserReadDOMEmptyHTMLText is browser_read_dom's answer for a match with
// no outer HTML at all.
func BrowserReadDOMEmptyHTMLText() string { return Text(browserReadDOMEmptyHTMLKey) }

type browserReadDOMTruncatedHintInput struct{ Shown int }

var browserReadDOMTruncatedHintKey = Define("tool_results.browser_read_dom_truncated_hint", browserReadDOMTruncatedHintInput{Shown: 100000})

// BrowserReadDOMTruncatedHintText is appended to browser_read_dom's HTML
// length note when the returned HTML was cut short.
func BrowserReadDOMTruncatedHintText(shown int) string {
	return browserReadDOMTruncatedHintKey.Render(browserReadDOMTruncatedHintInput{Shown: shown})
}

var browserReadDOMHiddenNoteKey = Define[struct{}]("tool_results.browser_read_dom_hidden_note", struct{}{})

// BrowserReadDOMHiddenNoteText prefixes browser_read_dom's content when the
// matched element is in the DOM but not visible.
func BrowserReadDOMHiddenNoteText() string { return Text(browserReadDOMHiddenNoteKey) }

type browserScreenshotBrokenImagesInput struct {
	Count int
	List  string
}

var browserScreenshotBrokenImagesKey = Define("tool_results.browser_screenshot_broken_images",
	browserScreenshotBrokenImagesInput{Count: 1, List: "/logo.png"})

// BrowserScreenshotBrokenImagesText is browser_screenshot's warning when the
// page has one or more <img> elements that failed to load.
func BrowserScreenshotBrokenImagesText(count int, list string) string {
	return browserScreenshotBrokenImagesKey.Render(browserScreenshotBrokenImagesInput{Count: count, List: list})
}

type browserScreenshotLoadingImagesInput struct{ Count int }

var browserScreenshotLoadingImagesKey = Define("tool_results.browser_screenshot_loading_images", browserScreenshotLoadingImagesInput{Count: 1})

// BrowserScreenshotLoadingImagesText is browser_screenshot's note when
// images were still loading at capture time.
func BrowserScreenshotLoadingImagesText(count int) string {
	return browserScreenshotLoadingImagesKey.Render(browserScreenshotLoadingImagesInput{Count: count})
}

var browserViewportNoElementPinnedKey = Define[struct{}]("tool_results.browser_viewport_no_element_pinned", struct{}{})

// BrowserViewportNoElementPinnedText is browser_set_viewport's answer when
// the page scrolls horizontally but no single overflowing element was found.
func BrowserViewportNoElementPinnedText() string { return Text(browserViewportNoElementPinnedKey) }

var browserViewportOverflowTailKey = Define[struct{}]("tool_results.browser_viewport_overflow_tail", struct{}{})

// BrowserViewportOverflowTailText closes browser_set_viewport's list of
// overflowing elements.
func BrowserViewportOverflowTailText() string { return Text(browserViewportOverflowTailKey) }
