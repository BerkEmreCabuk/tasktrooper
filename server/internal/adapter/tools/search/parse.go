package search

import (
	"encoding/base64"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// Both parsers run on the HTML tokenizer rather than on string offsets. The old
// hand-rolled scanner matched `class="result__a"` literally, so a reordered
// attribute, an extra class or a newline inside the tag silently produced zero
// results — which is indistinguishable, from the outside, from being blocked.
// The tokenizer also unescapes entities and attribute values for free, which is
// what makes Bing's &amp;-riddled redirect hrefs parseable at all.

// botWallMarkers are compound on purpose. Bare "anomaly" or "challenge" appear
// in ordinary result snippets — searching for "coding challenge" would have
// tripped a substring check on the word alone and thrown away a good page.
var botWallMarkers = []string{
	"anomaly.js",
	"anomaly-modal",
	"assets/anomaly",
	"challenge-form",
	"challenge-submit",
	"captcha-delivery",
	"/sorry/index",
}

// isBotWall reports whether a page is a challenge rather than an answer.
//
// Only ask it about a page the parser found nothing in. Both backends echo the
// query into the search box as value="…", so a query that happens to contain a
// marker — "anomaly.js", "challenge-form" — puts that marker on its own result
// page, and asking first made those queries unanswerable.
func isBotWall(page string) bool {
	lower := strings.ToLower(page)
	for _, marker := range botWallMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// ─── DuckDuckGo ───────────────────────────────────────────────────────────────

// parseDuckDuckGo reads the html.duckduckgo.com result list: a title anchor
// carrying class result__a, then a snippet anchor carrying class
// result__snippet, repeated.
func parseDuckDuckGo(page string) []result {
	const (
		idle = iota
		inTitle
		inSnippet
	)

	z := html.NewTokenizer(strings.NewReader(page))
	var (
		out     []result
		cur     *result
		title   strings.Builder
		snippet strings.Builder
		state   = idle
	)

	flush := func() {
		if cur != nil {
			cur.Title = squash(title.String())
			cur.Snippet = squash(snippet.String())
			if cur.Title != "" && cur.URL != "" {
				out = append(out, *cur)
			}
		}
		cur = nil
		state = idle
		title.Reset()
		snippet.Reset()
	}

	for {
		switch z.Next() {
		case html.ErrorToken:
			flush()
			return dedupe(out)

		case html.StartTagToken, html.SelfClosingTagToken:
			t := z.Token()
			if t.Data != "a" {
				continue
			}
			switch {
			case hasClass(t, "result__a"):
				flush()
				href := unwrapDuckDuckGo(attr(t, "href"))
				if href == "" {
					continue // an ad, or a link with nothing behind it
				}
				cur = &result{URL: href}
				state = inTitle
			case cur != nil && hasClass(t, "result__snippet"):
				state = inSnippet
			}

		case html.TextToken:
			switch state {
			case inTitle:
				title.WriteString(z.Token().Data)
			case inSnippet:
				snippet.WriteString(z.Token().Data)
			}

		case html.EndTagToken:
			if z.Token().Data != "a" {
				continue
			}
			if state == inSnippet {
				flush() // the snippet closes the result
				continue
			}
			state = idle
		}
	}
}

// unwrapDuckDuckGo returns the destination behind a result href, or "" when the
// link is not a result at all. DuckDuckGo serves either the target directly or
// its //duckduckgo.com/l/?uddg=<encoded>&rut=... wrapper; y.js is an ad.
func unwrapDuckDuckGo(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if host := strings.ToLower(u.Hostname()); host == "duckduckgo.com" || strings.HasSuffix(host, ".duckduckgo.com") {
		if strings.Contains(u.Path, "y.js") {
			return ""
		}
		if uddg := u.Query().Get("uddg"); strings.HasPrefix(uddg, "http") {
			return uddg
		}
		if strings.HasPrefix(u.Path, "/l/") {
			return "" // a redirect we cannot read is not a result
		}
	}
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return ""
	}
	return raw
}

// ─── Bing ─────────────────────────────────────────────────────────────────────

// parseBing reads the b_algo list: one <li class="b_algo"> per result, its
// title in the first <h2><a href>, its snippet in the first <p>.
func parseBing(page string) []result {
	z := html.NewTokenizer(strings.NewReader(page))
	var (
		out        []result
		cur        *result
		title      strings.Builder
		snippet    strings.Builder
		collecting *strings.Builder
		liDepth    int
		h2Depth    int
		titleDone  bool
	)

	flush := func() {
		if cur != nil {
			cur.Title = squash(title.String())
			cur.Snippet = squash(snippet.String())
			if cur.Title != "" && cur.URL != "" {
				out = append(out, *cur)
			}
		}
		cur = nil
		collecting = nil
		liDepth, h2Depth = 0, 0
		titleDone = false
		title.Reset()
		snippet.Reset()
	}

	for {
		switch z.Next() {
		case html.ErrorToken:
			flush()
			return dedupe(out)

		case html.StartTagToken:
			t := z.Token()
			switch t.Data {
			case "li":
				if hasClass(t, "b_algo") {
					flush()
					cur = &result{}
					liDepth = 1
					continue
				}
				if cur != nil {
					liDepth++
				}
			case "h2":
				if cur != nil {
					h2Depth++
				}
			case "a":
				if cur == nil || h2Depth == 0 || titleDone {
					continue
				}
				if href := unwrapBing(attr(t, "href")); href != "" {
					cur.URL = href
					collecting = &title
				}
			case "p":
				if cur != nil && collecting == nil && snippet.Len() == 0 {
					collecting = &snippet
				}
			}

		case html.TextToken:
			if collecting != nil {
				collecting.WriteString(z.Token().Data)
			}

		case html.EndTagToken:
			switch z.Token().Data {
			case "a":
				if collecting == &title {
					collecting, titleDone = nil, true
				}
			case "p":
				if collecting == &snippet {
					collecting = nil
				}
			case "h2":
				if h2Depth > 0 {
					h2Depth--
				}
			case "li":
				if cur != nil {
					if liDepth--; liDepth <= 0 {
						flush()
					}
				}
			}
		}
	}
}

// unwrapBing returns the destination behind a Bing result href. Bing wraps most
// results in www.bing.com/ck/a?...&u=a1<base64url of the real URL>; when that
// decodes the target is used, and when it does not the wrapper is kept rather
// than dropping a result on the floor. /aclick and /aclk are ads.
func unwrapBing(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if host := strings.ToLower(u.Hostname()); host == "bing.com" || strings.HasSuffix(host, ".bing.com") {
		switch {
		case strings.HasPrefix(u.Path, "/aclick"), strings.HasPrefix(u.Path, "/aclk"):
			return ""
		case strings.HasPrefix(u.Path, "/ck/a"):
			if target := decodeBingRedirect(u.Query().Get("u")); target != "" {
				return target
			}
			return raw
		}
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return ""
	}
	return raw
}

func decodeBingRedirect(u string) string {
	if u == "" {
		return ""
	}
	// The "a1" prefix is a version tag, not part of the payload.
	u = strings.TrimPrefix(u, "a1")
	for _, enc := range []*base64.Encoding{
		base64.RawURLEncoding, base64.URLEncoding,
		base64.RawStdEncoding, base64.StdEncoding,
	} {
		decoded, err := enc.DecodeString(u)
		if err != nil {
			continue
		}
		if s := string(decoded); strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
			return s
		}
	}
	return ""
}

// ─── shared ───────────────────────────────────────────────────────────────────

func attr(t html.Token, name string) string {
	for _, a := range t.Attr {
		if strings.EqualFold(a.Key, name) {
			return a.Val
		}
	}
	return ""
}

// hasClass matches one class out of a whitespace-separated list, so an extra
// class or a different attribute order cannot hide a result.
func hasClass(t html.Token, want string) bool {
	for _, c := range strings.Fields(attr(t, "class")) {
		if c == want {
			return true
		}
	}
	return false
}

// squash turns collected text into one clean line.
//
// The unescape is a second pass on purpose. The tokenizer already decodes the
// entities it sees, but a backend that escaped its snippet twice ships
// "&amp;nbsp;", which decodes once to the literal text "&nbsp;" — that string
// reached live output inside a snippet ("19.12.&nbsp;Lock Management"). The
// leftovers of a double escape are worth a second pass; there is no third.
//
// strings.Fields then splits on U+00A0 along with the ASCII whitespace —
// unicode.IsSpace counts NBSP — so the non-breaking space an entity decodes to
// becomes an ordinary one instead of a stray byte in the middle of a title.
func squash(s string) string {
	return strings.Join(strings.Fields(html.UnescapeString(s)), " ")
}

// pageHasClass reports whether any tag in the page carries class token want.
//
// The marker checks used strings.Contains, which is how "b_no" — Bing's
// no-results container — matched b_notificationContainer on every Bing page
// ever served, including the walls. Matching the token means a marker can be a
// prefix of another class without silently answering yes.
func pageHasClass(page, want string) bool {
	if want == "" {
		return false
	}
	z := html.NewTokenizer(strings.NewReader(page))
	for {
		switch z.Next() {
		case html.ErrorToken:
			return false
		case html.StartTagToken, html.SelfClosingTagToken:
			if hasClass(z.Token(), want) {
				return true
			}
		}
	}
}

// dedupe keeps the first result per URL: both backends repeat a destination
// across a result and its sitelinks.
func dedupe(in []result) []result {
	seen := make(map[string]struct{}, len(in))
	out := in[:0]
	for _, r := range in {
		if _, ok := seen[r.URL]; ok {
			continue
		}
		seen[r.URL] = struct{}{}
		out = append(out, r)
	}
	return out
}
