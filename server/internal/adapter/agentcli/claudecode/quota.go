package claudecode

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// usageLimitPattern matches the CLI saying the subscription is spent, in the
// spellings it has actually used. It is deliberately loose about what comes
// before "usage limit reached" ("Claude AI usage limit reached", "5-hour usage
// limit reached", the bare phrase) and deliberately strict about the phrase
// itself.
//
// Why match text at all, rather than a status code: the limit arrives as an
// error MESSAGE — on the result event, or on stderr when the CLI gives up
// before emitting one — and there is no distinct exit code for it. The cost of
// a false positive is one task parked for half an hour instead of failing; the
// cost of a false negative is a run failed for a billing window, which spends
// one of the task's three consecutive-failure lives and loses the CLI session
// that had the work in it. The loose match is the cheaper error.
var usageLimitPattern = regexp.MustCompile(`(?i)usage limit reached|usage limit exceeded|exceeded your (?:usage|rate) limit|usage credit limit reached|reached your weekly usage limit|hit your (?:usage )?limit|usage limit resets`)

// resetEpochPattern pulls the reset time out of the format the CLI has emitted
// historically: "Claude AI usage limit reached|1712345678". The number is a
// Unix epoch — seconds when it is 10 digits, milliseconds when it is 13, which
// is why the width decides the unit rather than a guess about magnitude.
var resetEpochPattern = regexp.MustCompile(`usage limit reached\s*\|\s*(\d{9,13})`)

// resumeMissingPattern matches the CLI refusing a --resume because it has no
// such conversation. Probed against v2.1.220, which writes to stderr
//
//	No conversation found with session ID: 00000000-1111-2222-3333-444444444444
//
// and then emits a result event with subtype "error_during_execution",
// is_error true and num_turns 0 — a shape indistinguishable from any other
// early failure, which is why the text is what identifies it.
//
// Loose about the wording around it ("No conversation found with session ID",
// "no conversation found for session") and strict about the two words that
// carry the meaning. The asymmetry is chosen the opposite way round from the
// quota pattern: a false positive here throws away a live session and restarts
// the conversation at full context cost, so this one stays narrow.
var resumeMissingPattern = regexp.MustCompile(`(?i)no conversation found|no session found|session .{0,40}not found`)

// quotaBlockFrom builds the park from whatever the CLI said, or nil when
// nothing on the session — structured or textual — says the usage limit was
// hit.
//
// Structured-first: a "rejected" rate_limit_event or a 429 result is the CLI's
// own word for the limit, not a guess from an error sentence, so both are
// checked before the text falls back to usageLimitPattern. stderrTail and the
// session's own text are joined for the text match because the limit has
// arrived on either depending on where the CLI gave up.
//
// now is a parameter rather than time.Now() so the fallback window is testable
// and so a single run cannot get two different "now"s while it decides.
func quotaBlockFrom(out outcome, stderrTail, sessionID string, now time.Time) *domain.QuotaBlock {
	if out.RateLimit != nil && out.RateLimit.Status == "rejected" {
		resumeAt := out.RateLimit.resetTime(now)
		if resumeAt.IsZero() {
			resumeAt = now.Add(domain.DefaultQuotaParkWindow)
		}
		return &domain.QuotaBlock{
			ResumeAt:     resumeAt,
			CLISessionID: sessionID,
			Detail:       fmt.Sprintf("Claude subscription limit (%s window) reached", out.RateLimit.RateLimitType),
			Provider:     domain.LLMProviderClaudeCode,
		}
	}
	if out.APIErrorStatus == 429 {
		resumeAt := out.RateLimit.resetTime(now)
		if resumeAt.IsZero() {
			resumeAt = now.Add(domain.DefaultQuotaParkWindow)
		}
		detail := "Claude subscription limit reached"
		if out.RateLimit != nil {
			detail = fmt.Sprintf("Claude subscription limit (%s window) reached", out.RateLimit.RateLimitType)
		}
		return &domain.QuotaBlock{
			ResumeAt:     resumeAt,
			CLISessionID: sessionID,
			Detail:       detail,
			Provider:     domain.LLMProviderClaudeCode,
		}
	}

	text := strings.Join([]string{out.Text, stderrTail}, "\n")
	if !usageLimitPattern.MatchString(text) {
		return nil
	}
	resumeAt, ok := parseResetEpoch(text)
	if !ok || !resumeAt.After(now) {
		// No epoch in the text, or one already in the past. A structured window
		// is consulted before giving up to the default: the CLI can emit a
		// rate_limit_event with a real reset alongside a text message that
		// carries none.
		if structured := out.RateLimit.resetTime(now); !structured.IsZero() {
			resumeAt = structured
		} else {
			// A stale epoch would make the sweeper resume immediately, hit the
			// same limit, and park again in a tight loop. The default window is
			// the floor either way.
			resumeAt = now.Add(domain.DefaultQuotaParkWindow)
		}
	}
	return &domain.QuotaBlock{
		ResumeAt:     resumeAt,
		CLISessionID: sessionID,
		Detail:       quotaDetail(text),
		Provider:     domain.LLMProviderClaudeCode,
	}
}

// parseResetEpoch reads the "|<epoch>" suffix. The bool is false when there is
// none, which is a normal outcome, not an error: the caller has a default.
func parseResetEpoch(text string) (time.Time, bool) {
	m := resetEpochPattern.FindStringSubmatch(text)
	if len(m) != 2 {
		return time.Time{}, false
	}
	raw, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || raw <= 0 {
		return time.Time{}, false
	}
	if len(m[1]) >= 13 {
		return time.UnixMilli(raw), true
	}
	return time.Unix(raw, 0), true
}

// quotaDetailMax bounds what goes onto the board card. The CLI's message is one
// sentence, but the text searched also contains a stderr tail, and a card is
// not a log viewer.
const quotaDetailMax = 300

// quotaDetail extracts the sentence that actually mentions the limit, so the
// card says what happened rather than showing the tail of an unrelated build
// log that happened to be in the same buffer.
func quotaDetail(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && usageLimitPattern.MatchString(line) {
			return domain.TruncateHead(line, quotaDetailMax)
		}
	}
	return "Claude Code usage limit reached"
}
