package cursor

import (
	"regexp"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cli/core"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

var usageLimitPattern = regexp.MustCompile(`(?i)usage limit reached|usage limit exceeded|hit your usage limit|spend limit hit|exceeded your (?:usage|rate) limit|rate limit exceeded`)

var resetDatePattern = regexp.MustCompile(`(?i)resets?(?:[^.\n]{0,40})?\son\s+(\d{1,2}/\d{1,2}/\d{2,4})`)

const quotaDetailMax = 300

func quotaBlockFrom(out core.Outcome, stderrTail, sessionID string, now time.Time) *domain.QuotaBlock {
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

func quotaDetail(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && usageLimitPattern.MatchString(line) {
			return domain.TruncateHead(line, quotaDetailMax)
		}
	}
	return "Cursor usage limit reached"
}