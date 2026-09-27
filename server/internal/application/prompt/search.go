package prompt

type searchBackendsUnavailableInput struct{ Reasons string }

var searchBackendsUnavailableKey = Define("guard.search_backends_unavailable", searchBackendsUnavailableInput{Reasons: "duckduckgo: timeout; bing: blocked"})

// SearchBackendsUnavailableText is web_search's refusal when every backend
// it tried failed.
func SearchBackendsUnavailableText(reasons string) string {
	return searchBackendsUnavailableKey.Render(searchBackendsUnavailableInput{Reasons: reasons})
}
