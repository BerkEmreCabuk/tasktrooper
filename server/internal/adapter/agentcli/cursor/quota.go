package cursor

import (
	"regexp"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// usageLimitPattern matches cursor-agent saying the account's Pro usage is
// spent, in the spellings reported against the CLI ("Error: You've hit your
// usage limit", the classified server error's own "usage limit"/"spend
// limit" wording). There is no distinct exit code for it — the CLI reports it
// as a "result" event with is_error true and the sentence in result, or on
// stderr when the session dies before emitting one — so text is what
// identifies it, the same tradeoff claudecode's usageLimitPattern documents.
var usageLimitPattern = regexp.MustCompile(`(?i)usage limit reached|usage limit exceeded|hit your usage limit|spend limit hit|exceeded your (?:usage|rate) limit|rate limit exceeded`)

// resetDatePattern pulls the reset date out of the one format cursor-agent has
// been reported to use: "resets ... on 8/14/2025" (month/day/year, cycle
// granularity — no hour). Unlike Claude Code's Unix-epoch reset, this is only
// ever a calendar day, so a caller that cannot resolve a full instant from it
// falls back to the default window.
var resetDatePattern = regexp.MustCompile(`(?i)resets?(?:[^.\n]{0,40})?\son\s+(\d{1,2}/\d{1,2}/\d{2,4})`)

const quotaDetailMax = 300

// quotaBlockFrom builds the park from a finished cursor-agent session, or nil
// when nothing in its result text or stderr says the usage limit was hit.
//
// now is a parameter rather than time.Now() so the fallback window is
// testable, mirroring claudecode's quotaBlockFrom.
func quotaBlockFrom(out outcome, stderrTail, sessionID string, now time.Time) *domain.QuotaBlock {
	text := strings.Join([]string{out.Text, stderrTail}, "\n")
	if !usageLimitPattern.MatchString(text) {
		return nil
	}
	resumeAt, ok := parseResetDate(text, now)
	if !ok {
		resumeAt = now.Add(domain.DefaultQuotaParkWindow)
	}
	return &domain.QuotaBlock{
		ResumeAt:     resumeAt,
		CLISessionID: sessionID,
		Detail:       quotaDetail(text),
		Provider:     domain.LLMProviderCursorAgent,
	}
}

// parseResetDate reads the "M/D/YYYY" suffix cursor-agent has been observed
// to emit. A date already in the past is treated as unparsed — trusting it
// would resume the run immediately, hit the same limit and park again in a
// tight loop — so the caller's default window is the floor either way.
func parseResetDate(text string, now time.Time) (time.Time, bool) {
	m := resetDatePattern.FindStringSubmatch(text)
	if len(m) != 2 {
		return time.Time{}, false
	}
	for _, layout := range []string{"1/2/2006", "1/2/06"} {
		t, err := time.ParseInLocation(layout, m[1], time.Local)
		if err != nil {
			continue
		}
		if !t.After(now) {
			return time.Time{}, false
		}
		return t, true
	}
	return time.Time{}, false
}

// quotaDetail extracts the line that actually mentions the limit, so the
// board card says what happened rather than showing an unrelated stderr tail.
func quotaDetail(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && usageLimitPattern.MatchString(line) {
			return domain.TruncateHead(line, quotaDetailMax)
		}
	}
	return "Cursor usage limit reached"
}
