package domain

import (
	"regexp"
	"strings"
)

// Memory is what an agent still needs to know weeks from now, on a task nobody
// has written yet. Runs kept filing something else into it: the state of the
// card they were on — "T-28 PR #8 head b06f146 fixed the gap I flagged", "moved
// code_review→ready_for_qa despite gh pr checks showing red". Every one of those
// is true for about an hour and is already recorded where it belongs (the task's
// comments and its event history), and because recall pulls a handful of
// memories into every future run, they crowd out the facts that were worth
// keeping — the same billing block written five times, each pinned to a
// different card.
//
// The test applied here is anchoring, because it is the one an agent can act on
// without judgement: a note that names a task key, a PR number, a commit SHA or
// a column transition is about one card. The durable version of the same lesson
// exists — it is that sentence with the card taken out ("this org's GitHub
// Actions billing is blocked, so CI check runs fail in seconds with a billing
// annotation; a human has to fix the billing, no code change helps") — and that
// is what the agent is told to write instead.

// A board key: T-28, B-3, DE-14. Standards and protocol names take the same
// shape, so the ones that turn up in real engineering notes are excluded rather
// than left to produce false rejections.
var (
	taskKeyPattern = regexp.MustCompile(`\b([A-Z]{1,4})-(\d{1,6})\b`)
	// Only multi-letter standards names are excluded. Single letters are left
	// in: the board's key prefix is per workspace, so "T-28" and "S-4" are both
	// plausible task keys and neither is a standard.
	notTaskKeys = map[string]bool{
		"UTF": true, "HTTP": true, "SHA": true, "ISO": true, "RFC": true, "MD": true,
		"IPV": true, "TLS": true, "AES": true, "RSA": true, "SPF": true, "IEEE": true,
		"PEP": true, "GPT": true, "CVE": true, "SQL": true, "OAUTH": true, "ES": true,
	}
)

type memoryDurabilityRule struct {
	pattern *regexp.Regexp
	reason  string
}

var memoryDurabilityRules = []memoryDurabilityRule{
	{
		// "PR #8", "pull request #12"
		pattern: regexp.MustCompile(`(?i)\b(?:pr|pull request)\s*#\s*\d+`),
		reason:  "it is pinned to one pull request",
	},
	{
		// A commit SHA: hex, long enough not to be a word, and mixed enough not
		// to be a plain number or a version.
		pattern: regexp.MustCompile(`\b(?:[0-9a-f]{7,40})\b`),
		reason:  "it quotes a commit SHA",
	},
	{
		// "moved code_review→ready_for_qa", "bounced in_qa -> need_revision"
		pattern: regexp.MustCompile(`(?i)\b(?:backlog|todo|in_progress|code_review|ready_for_qa|in_qa|pm_uat|analiz_review|need_revision|blocked|done|released)\b\s*(?:→|->|to)\s*\b(?:backlog|todo|in_progress|code_review|ready_for_qa|in_qa|pm_uat|analiz_review|need_revision|blocked|done|released)\b`),
		reason:  "it records one card's column move",
	},
	{
		// "this task", "this run", "the task I am on"
		pattern: regexp.MustCompile(`(?i)\bthis (?:task|run|card|ticket|pr|review round)\b`),
		reason:  "it is about the run you are in, not about anything a later run can reuse",
	},
	{
		// "feature/t-28", "tt-123"
		pattern: regexp.MustCompile(`(?i)\b(?:feature/|branch\s+)?tt?-\d{1,6}\b`),
		reason:  "it names one task's branch",
	},
}

// mixedHex keeps the SHA rule from firing on a plain number or a word made of
// hex letters ("deadbeef" is a SHA-shaped joke; "1234567" is a number).
func memoryLooksLikeSHA(text string) bool {
	for _, match := range regexp.MustCompile(`\b[0-9a-f]{7,40}\b`).FindAllString(text, -1) {
		hasDigit, hasLetter := false, false
		for _, r := range match {
			if r >= '0' && r <= '9' {
				hasDigit = true
			} else {
				hasLetter = true
			}
		}
		if hasDigit && hasLetter {
			return true
		}
	}
	return false
}

func memoryMentionsTaskKey(text string) bool {
	for _, m := range taskKeyPattern.FindAllStringSubmatch(text, -1) {
		if !notTaskKeys[strings.ToUpper(m[1])] {
			return true
		}
	}
	return false
}

// runLogReason reports why this content is a run log rather than a memory, or
// "" when it is worth keeping.
func MemoryRunLogReason(content string) string {
	text := strings.TrimSpace(content)
	if text == "" {
		return ""
	}
	if memoryMentionsTaskKey(text) {
		return "it names a specific board task"
	}
	for _, rule := range memoryDurabilityRules {
		if rule.pattern == nil {
			continue
		}
		if rule.reason == "it quotes a commit SHA" {
			if memoryLooksLikeSHA(text) {
				return rule.reason
			}
			continue
		}
		if rule.pattern.MatchString(text) {
			return rule.reason
		}
	}
	return ""
}

const MemoryRunLogHint = "Memory is for what a DIFFERENT task will need, months from now. " +
	"What happened on the card you are working — what you verified, what you moved, which commit fixed it — belongs in that task's comments (add_task_comment), where it is already recorded and where people look for it. " +
	"If there is a durable fact underneath, save that instead, with the card taken out of it: not \"T-28's checks failed on billing\" but \"this org's GitHub Actions billing is blocked: check runs fail within seconds with a billing annotation and only a human can clear it\"."

// memoryTokenSet reduces a memory to the words that carry its meaning, so two notes
// that say the same thing in a slightly different order are recognisably the
// same note.
func memoryTokenSet(text string) map[string]struct{} {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	set := make(map[string]struct{}, len(fields))
	for _, f := range fields {
		if len(f) < 3 {
			continue // "the", "a", "is" — noise in a similarity test
		}
		set[f] = struct{}{}
	}
	return set
}

// similarity is the Jaccard overlap of two memories' word sets: 1 means the same
// words, 0 means nothing in common.
func memorySimilarity(a, b string) float64 {
	setA, setB := memoryTokenSet(a), memoryTokenSet(b)
	if len(setA) == 0 || len(setB) == 0 {
		return 0
	}
	shared := 0
	for word := range setA {
		if _, ok := setB[word]; ok {
			shared++
		}
	}
	union := len(setA) + len(setB) - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}

// memoryDuplicateThreshold is deliberately high: refusing a save is only correct when
// the memory really is already there. A related-but-different lesson must go in.
const memoryDuplicateThreshold = 0.6

// duplicateOf returns the existing memory this content merely repeats, if any.
func MemoryDuplicateOf(existing []AgentMemory, content string) (AgentMemory, bool) {
	best := AgentMemory{}
	bestScore := 0.0
	for _, mem := range existing {
		if score := memorySimilarity(mem.Content, content); score > bestScore {
			best, bestScore = mem, score
		}
	}
	if bestScore >= memoryDuplicateThreshold {
		return best, true
	}
	return AgentMemory{}, false
}
