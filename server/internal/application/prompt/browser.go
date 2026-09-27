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
