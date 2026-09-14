package search

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// The fixtures are real answers captured from the live endpoints. They are the
// only honest way to test this: every past breakage of this tool was markup
// drifting away from what the parser assumed, and a hand-written sample encodes
// the assumption rather than checking it.
func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(b)
}

func TestParseDuckDuckGoFixture(t *testing.T) {
	results := parseDuckDuckGo(fixture(t, "ddg-html-post.html"))

	if len(results) < 8 {
		t.Fatalf("got %d results, want at least 8", len(results))
	}
	for i, r := range results {
		if r.Title == "" {
			t.Errorf("result %d has no title", i)
		}
		if !strings.HasPrefix(r.URL, "https://") && !strings.HasPrefix(r.URL, "http://") {
			t.Errorf("result %d URL is not absolute: %q", i, r.URL)
		}
		if strings.Contains(r.URL, "duckduckgo.com/l/") || strings.Contains(r.URL, "uddg=") {
			t.Errorf("result %d URL is still wrapped: %q", i, r.URL)
		}
		if r.Snippet == "" {
			t.Errorf("result %d (%s) has no snippet", i, r.URL)
		}
		if strings.Contains(r.Title, "<") || strings.Contains(r.Snippet, "<") {
			t.Errorf("result %d still carries markup: %q / %q", i, r.Title, r.Snippet)
		}
	}
	if got := results[0].URL; got != "https://docs.gofiber.io/contrib/websocket/" {
		t.Errorf("first URL = %q", got)
	}
	if got := results[0].Title; got != "Websocket - Fiber" {
		t.Errorf("first title = %q", got)
	}
}

func TestParseBingFixture(t *testing.T) {
	results := parseBing(fixture(t, "bing-html.html"))

	if len(results) < 5 {
		t.Fatalf("got %d results, want at least 5", len(results))
	}
	decoded := 0
	for i, r := range results {
		if r.Title == "" || r.URL == "" {
			t.Errorf("result %d is incomplete: %+v", i, r)
		}
		if !strings.Contains(r.URL, "bing.com/ck/a") {
			decoded++
		}
		if strings.Contains(r.Title, "&#") || strings.Contains(r.Snippet, "&#") {
			t.Errorf("result %d kept an undecoded entity: %q / %q", i, r.Title, r.Snippet)
		}
	}
	if decoded != len(results) {
		t.Errorf("%d of %d results are still Bing redirect wrappers", len(results)-decoded, len(results))
	}
	if got := results[0].URL; !strings.HasPrefix(got, "https://support.google.com/translate/") {
		t.Errorf("first URL = %q", got)
	}
	if results[0].Snippet == "" {
		t.Error("first result has no snippet")
	}
}

// The anomaly page is what every GET against html.duckduckgo.com answered from
// this network: HTTP 202, a challenge, and nothing that parses.
func TestBotWallDetection(t *testing.T) {
	anomaly := fixture(t, "ddg-anomaly-202.html")

	if !isBotWall(anomaly) {
		t.Error("the anomaly page was not recognised as a bot wall")
	}
	if got := parseDuckDuckGo(anomaly); len(got) != 0 {
		t.Errorf("the anomaly page parsed to %d results", len(got))
	}
	if !strings.Contains(anomaly, "anomaly") {
		t.Fatal("fixture no longer contains the marker it exists for")
	}

	for _, name := range []string{"ddg-html-post.html", "bing-html.html"} {
		if isBotWall(fixture(t, name)) {
			t.Errorf("%s was misread as a bot wall", name)
		}
	}
}

// A snippet that merely says "challenge" is not a challenge page. The bare word
// is why the markers are compound.
func TestBotWallIgnoresTheWordsOnTheirOwn(t *testing.T) {
	page := `<a class="result__a" href="https://example.com/x">Advent of Code</a>` +
		`<a class="result__snippet">A daily coding challenge; anomaly detection puzzles.</a>`
	if isBotWall(page) {
		t.Error("an ordinary result page was read as a bot wall")
	}
	if got := parseDuckDuckGo(page); len(got) != 1 {
		t.Fatalf("got %d results, want 1", len(got))
	}
}

// The old parser matched `class="result__a"` as a literal substring, so a
// reordered attribute or a newline inside the tag produced zero results — which
// looks exactly like being blocked.
func TestParsingToleratesAttributeOrderAndWhitespace(t *testing.T) {
	page := "<div>\n<a\n  href='https://example.com/one'\n  rel=\"nofollow\"\n  class=\"js-result-title-link result__a\"\n>One &amp; Only</a>\n" +
		"<a class=\"result__snippet js-result-snippet\" href='https://example.com/one'>First <b>snippet</b>\n  here</a>\n</div>"

	got := parseDuckDuckGo(page)
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1: %+v", len(got), got)
	}
	if got[0].URL != "https://example.com/one" {
		t.Errorf("URL = %q", got[0].URL)
	}
	if got[0].Title != "One & Only" {
		t.Errorf("title = %q", got[0].Title)
	}
	if got[0].Snippet != "First snippet here" {
		t.Errorf("snippet = %q", got[0].Snippet)
	}
}

func TestUnwrapDuckDuckGo(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"https://example.com/a", "https://example.com/a"},
		{"//duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fb%3Fx%3D1&rut=abc", "https://example.com/b?x=1"},
		{"https://duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com%2Fc", "https://example.com/c"},
		{"//duckduckgo.com/y.js?ad_domain=x&u3=y", ""},
		{"https://duckduckgo.com/l/?rut=abc", ""},
		{"/settings", ""},
		{"", ""},
		{"//example.com/d", "https://example.com/d"},
	} {
		if got := unwrapDuckDuckGo(tc.raw); got != tc.want {
			t.Errorf("unwrapDuckDuckGo(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestUnwrapBing(t *testing.T) {
	// u=a1<base64url> of https://example.com/e
	const encoded = "https://www.bing.com/ck/a?!&&p=x&u=a1aHR0cHM6Ly9leGFtcGxlLmNvbS9l&ntb=1"
	const undecodable = "https://www.bing.com/ck/a?!&&p=x&u=a1%%%not-base64&ntb=1"

	for _, tc := range []struct{ raw, want string }{
		{"https://example.com/a", "https://example.com/a"},
		{encoded, "https://example.com/e"},
		{undecodable, undecodable}, // kept: a wrapper is better than a dropped result
		{"https://www.bing.com/aclick?ld=x", ""},
		{"/search?q=next", ""},
		{"", ""},
	} {
		if got := unwrapBing(tc.raw); got != tc.want {
			t.Errorf("unwrapBing(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestDedupeKeepsTheFirstOfEachURL(t *testing.T) {
	got := dedupe([]result{
		{Title: "a", URL: "https://x.test"},
		{Title: "b", URL: "https://y.test"},
		{Title: "c", URL: "https://x.test"},
	})
	if len(got) != 2 || got[0].Title != "a" || got[1].Title != "b" {
		t.Fatalf("unexpected: %+v", got)
	}
}

// A page that says "b_no" somewhere is not a page that says "no results". Bing
// puts b_notificationContainer on every response it serves, walls included, so
// the substring test was true for every Bing page ever fetched — which is how a
// Bing challenge became a cached "No results found."
func TestPageHasClassMatchesWholeTokensOnly(t *testing.T) {
	bing := fixture(t, "bing-html.html")
	if !strings.Contains(bing, "b_notificationContainer") {
		t.Fatal("fixture no longer contains the class this test exists for")
	}
	if pageHasClass(bing, "b_no") {
		t.Error("b_notificationContainer was read as the b_no empty-results container")
	}
	if !pageHasClass(bing, "b_algo") {
		t.Error("the result marker was not found on a real Bing result page")
	}
	if !pageHasClass(`<ol id="b_results"><li class="b_no b_algoBorder"><h1>There are no results</h1></li></ol>`, "b_no") {
		t.Error("the real no-results container was not recognised")
	}
	if pageHasClass(bing, "") {
		t.Error("an empty marker matched")
	}
}

// A literal "&nbsp;" reached live output inside a snippet ("19.12.&nbsp;Lock
// Management"): the backend escaped its own escape, so one decoding pass left
// the entity spelled out as text.
func TestSnippetEntitiesAreFullyDecoded(t *testing.T) {
	page := `<a class="result__a" href="https://example.com/db">Chapter&nbsp;19.&nbsp;Server Configuration</a>` +
		`<a class="result__snippet">19.12.&amp;nbsp;Lock Management&amp;nbsp;&amp;#35;</a>`

	got := parseDuckDuckGo(page)
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1", len(got))
	}
	if got[0].Title != "Chapter 19. Server Configuration" {
		t.Errorf("title = %q", got[0].Title)
	}
	if got[0].Snippet != "19.12. Lock Management #" {
		t.Errorf("snippet = %q", got[0].Snippet)
	}
	for _, field := range []string{got[0].Title, got[0].Snippet} {
		if strings.Contains(field, "&nbsp;") || strings.Contains(field, "&#") {
			t.Errorf("an entity survived: %q", field)
		}
		if strings.Contains(field, "\u00a0") {
			t.Errorf("a non-breaking space survived: %q", field)
		}
	}
}

func TestSquashNormalisesNonBreakingSpace(t *testing.T) {
	if got := squash("19.12.\u00a0Lock\u00a0Management "); got != "19.12. Lock Management" {
		t.Errorf("squash = %q", got)
	}
}

// ─── politeness ───────────────────────────────────────────────────────────────

// With a clock that never moves, three requests take three different slots one
// gap apart — the reservation is what stops a burst from all measuring against
// the same last request and all leaving at once.
func TestLimiterHandsOutDistinctSlots(t *testing.T) {
	frozen := time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)
	var slept []time.Duration

	l := newLimiter(time.Second)
	l.now = func() time.Time { return frozen }
	l.sleep = func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}

	for i := 0; i < 3; i++ {
		if err := l.wait(context.Background()); err != nil {
			t.Fatalf("wait %d: %v", i, err)
		}
	}
	want := []time.Duration{time.Second, 2 * time.Second}
	if len(slept) != len(want) {
		t.Fatalf("slept %v, want %v", slept, want)
	}
	for i, d := range want {
		if slept[i] != d {
			t.Errorf("wait %d slept %s, want %s", i+1, slept[i], d)
		}
	}
}

// The gap used to be spent holding the mutex, so a caller behind the sleeper
// was parked on Lock — where a cancelled context cannot reach it — and a
// caller that had given up kept its goroutine for the rest of the gap.
func TestLimiterWaitIsInterruptibleAndDoesNotHoldTheLock(t *testing.T) {
	l := newLimiter(time.Hour)
	entered := make(chan struct{}, 4)
	sleep := l.sleep
	l.sleep = func(ctx context.Context, d time.Duration) error {
		entered <- struct{}{}
		return sleep(ctx, d)
	}

	if err := l.wait(context.Background()); err != nil {
		t.Fatalf("the first slot is free: %v", err)
	}

	parked, unpark := context.WithCancel(context.Background())
	defer unpark()
	parkedErr := make(chan error, 1)
	go func() { parkedErr <- l.wait(parked) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the second caller never reached the wait")
	}

	// The sleeper is an hour from its slot. A caller arriving behind it with a
	// dead context must answer now, not when the hour is up.
	dead, kill := context.WithCancel(context.Background())
	kill()
	behind := make(chan error, 1)
	go func() { behind <- l.wait(dead) }()
	select {
	case err := <-behind:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("wait returned %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wait queued on the lock while another caller slept")
	}

	unpark()
	select {
	case err := <-parkedErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("the parked wait returned %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling the context did not end the wait")
	}
}
