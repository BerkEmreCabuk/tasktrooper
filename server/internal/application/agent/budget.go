package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	budgetWarningTurns      = 3
	runTokenWarnRatio       = 0.85
	repeatNoteThreshold     = 2
	repeatAbortThreshold    = 4
	errStreakNoteThreshold  = 3
	errStreakAbortThreshold = 8
	toolErrorNoteThreshold  = 5
	sameCallNoteThreshold   = 3
	sameCallAbortThreshold  = 8
	lastCallsKept           = 5
	topFailingToolsKept     = 3
)

type RunStats struct {
	Iterations         int
	ToolCalls          int
	ByTool             map[string]int
	RepeatedNoProgress int
	SkippedRepeats     int
	ToolErrors         int
	ErrorsByTool       map[string]int
	MaxErrorStreak     int
	LastCalls          []string
}

func (s RunStats) Summary() string {
	if s.ToolCalls == 0 {
		return fmt.Sprintf("%d iterations, no tool calls", s.Iterations)
	}
	parts := make([]string, 0, len(s.ByTool))
	for _, p := range rankCounts(s.ByTool, 0) {
		parts = append(parts, fmt.Sprintf("%s×%d", p.name, p.count))
	}
	out := fmt.Sprintf("%d iterations, %d tool calls [%s]", s.Iterations, s.ToolCalls, strings.Join(parts, " "))
	if s.RepeatedNoProgress > 0 {
		out += fmt.Sprintf(", %d repeated no-progress calls", s.RepeatedNoProgress)
	}
	if s.SkippedRepeats > 0 {
		out += fmt.Sprintf(", %d repeats answered without re-running", s.SkippedRepeats)
	}
	if s.ToolErrors > 0 {
		out += fmt.Sprintf(", %d tool errors (%s), longest failing streak %d",
			s.ToolErrors, s.FailurePattern(), s.MaxErrorStreak)
	}
	if len(s.LastCalls) > 0 {
		out += ", last: " + strings.Join(s.LastCalls, " → ")
	}
	return out
}

func (s RunStats) FailurePattern() string {
	ranked := rankCounts(s.ErrorsByTool, topFailingToolsKept)
	if len(ranked) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ranked))
	for _, p := range ranked {
		parts = append(parts, fmt.Sprintf("%s failed %d×", p.name, p.count))
	}
	return strings.Join(parts, ", ")
}

func (s RunStats) ErrorRate() float64 {
	if s.ToolCalls == 0 {
		return 0
	}
	return float64(s.ToolErrors) / float64(s.ToolCalls) * 100
}

type countPair struct {
	name  string
	count int
}

func rankCounts(counts map[string]int, top int) []countPair {
	pairs := make([]countPair, 0, len(counts))
	for name, count := range counts {
		pairs = append(pairs, countPair{name, count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count != pairs[j].count {
			return pairs[i].count > pairs[j].count
		}
		return pairs[i].name < pairs[j].name
	})
	if top > 0 && len(pairs) > top {
		pairs = pairs[:top]
	}
	return pairs
}

type runStatsCarrier interface {
	RunStats() RunStats
}

func StatsFromError(err error) (RunStats, bool) {
	for err != nil {
		if carrier, ok := err.(runStatsCarrier); ok {
			return carrier.RunStats(), true
		}
		unwrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return RunStats{}, false
		}
		err = unwrapped.Unwrap()
	}
	return RunStats{}, false
}

type giveUpCause struct {
	Stuck          bool
	DeadEnd        bool
	TokenExhausted bool
	TokensUsed     int
}

type BudgetExhaustedError struct {
	Budget         int
	Stats          RunStats
	Partial        string
	Stuck          bool
	DeadEnd        bool
	TokenExhausted bool
	TokensUsed     int
}

func (e *BudgetExhaustedError) Error() string {
	switch {
	case e.DeadEnd:
		return fmt.Sprintf("agent loop stopped: %d tool calls in a row failed (%s)", errStreakAbortThreshold, e.Stats.Summary())
	case e.Stuck:
		return fmt.Sprintf("agent loop stopped: repeated the same call with no new result (%s)", e.Stats.Summary())
	case e.TokenExhausted:
		return fmt.Sprintf("run token budget exhausted after %d tokens (%s)", e.TokensUsed, e.Stats.Summary())
	default:
		return fmt.Sprintf("agent loop exceeded maximum iterations (%d): %s", e.Budget, e.Stats.Summary())
	}
}

func (e *BudgetExhaustedError) RunStats() RunStats { return e.Stats }

type ChatFailedError struct {
	Stats RunStats
	Err   error
}

func (e *ChatFailedError) Error() string {
	if rl, ok := domain.RateLimitOf(e.Err); ok {
		return rl.UserMessage()
	}
	return fmt.Sprintf("llm chat failed: %v (%s)", e.Err, e.Stats.Summary())
}

func (e *ChatFailedError) Unwrap() error      { return e.Err }
func (e *ChatFailedError) RunStats() RunStats { return e.Stats }

type callTracker struct {
	seen      map[string]*callRecord
	lastKey   string
	errStreak int
	stats     RunStats
}

type callRecord struct {
	resultHash string
	repeats    int
	execs      int
}

type callOutcome struct {
	Repeats    int
	Execs      int
	ErrStreak  int
	ToolErrors int
}

func newCallTracker() *callTracker {
	return &callTracker{
		seen:  map[string]*callRecord{},
		stats: RunStats{ByTool: map[string]int{}, ErrorsByTool: map[string]int{}},
	}
}

func callKey(name, args string) string {
	return name + "|" + strings.TrimSpace(args)
}

func (t *callTracker) canSkip(name, args string) bool {
	key := callKey(name, args)
	if key == "" || key != t.lastKey {
		return false
	}
	rec, ok := t.seen[key]
	return ok && rec.repeats >= 1
}

func (t *callTracker) observe(name, args, result string, isError bool) callOutcome {
	t.count(name, args, isError)

	key := callKey(name, args)
	hash := hashString(normalizeResult(result))
	rec, ok := t.seen[key]
	switch {
	case !ok:
		t.seen[key] = &callRecord{resultHash: hash, execs: 1}
	case rec.resultHash != hash:
		rec.resultHash = hash
		rec.repeats = 0
		rec.execs++
	default:
		rec.repeats++
		rec.execs++
		t.stats.RepeatedNoProgress++
	}
	t.lastKey = key

	return t.outcome(name, key)
}

func (t *callTracker) observeSkipped(name, args string) callOutcome {
	t.count(name, args, false)
	t.stats.SkippedRepeats++

	key := callKey(name, args)
	if rec, ok := t.seen[key]; ok {
		rec.repeats++
		rec.execs++
		t.stats.RepeatedNoProgress++
	}
	t.lastKey = key

	return t.outcome(name, key)
}

func (t *callTracker) count(name, args string, isError bool) {
	t.stats.ToolCalls++
	t.stats.ByTool[name]++
	t.stats.LastCalls = append(t.stats.LastCalls, name+"("+previewArgs(args)+")")
	if len(t.stats.LastCalls) > lastCallsKept {
		t.stats.LastCalls = t.stats.LastCalls[len(t.stats.LastCalls)-lastCallsKept:]
	}

	if !isError {
		t.errStreak = 0
		return
	}
	t.stats.ToolErrors++
	t.stats.ErrorsByTool[name]++
	t.errStreak++
	if t.errStreak > t.stats.MaxErrorStreak {
		t.stats.MaxErrorStreak = t.errStreak
	}
}

func (t *callTracker) outcome(name, key string) callOutcome {
	out := callOutcome{ErrStreak: t.errStreak, ToolErrors: t.stats.ErrorsByTool[name]}
	if rec, ok := t.seen[key]; ok {
		out.Repeats = rec.repeats
		out.Execs = rec.execs
	}
	return out
}

func (t *callTracker) snapshot(iterations int) RunStats {
	stats := t.stats
	stats.Iterations = iterations
	return stats
}

var volatile = []struct {
	pattern *regexp.Regexp
	with    string
}{
	{regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]"), ""},
	{regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d+)?(Z|\s?[+-]\d{2}:?\d{2})?`), "<ts>"},
	{regexp.MustCompile(`\b\d{1,2}:\d{2}:\d{2}(\.\d+)?\b`), "<ts>"},
	{regexp.MustCompile(`\b\d+m\s?\d+(\.\d+)?s\b`), "<dur>"},
	{regexp.MustCompile(`(?i)\b\d+(\.\d+)?\s?(ns|µs|us|ms|s|sec|secs|seconds|min|mins|h)\b`), "<dur>"},
	{regexp.MustCompile(`(?i)\b\d+(\.\d+)?\s?(b|kb|mb|gb|kib|mib|gib)\b`), "<size>"},
	{regexp.MustCompile(`\b\d+(\.\d+)?%`), "<pct>"},
}

func normalizeResult(s string) string {
	for _, v := range volatile {
		s = v.pattern.ReplaceAllString(s, v.with)
	}
	return strings.TrimSpace(s)
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

func previewArgs(args string) string {
	args = strings.Join(strings.Fields(args), " ")
	if len(args) > 80 {
		return args[:80] + "…"
	}
	return args
}

func budgetWarningMessage(remaining int) string {
	return fmt.Sprintf(
		"[budget] %d model turns left in this run. Stop exploring and stop re-reading files. "+
			"Apply the smallest change that completes the task now, then reply with a plain-text summary "+
			"of what you changed and what is left. Unfinished work is committed to the task branch and "+
			"picked up by the next run, so a clear summary is more useful than a rushed edit.", remaining)
}

func runTokenWarnThreshold(cap int) int {
	return int(float64(cap) * runTokenWarnRatio)
}

func tokenBudgetWarningMessage(used, cap int) string {
	return fmt.Sprintf(
		"[token budget] This run has used about %d of its %d token budget. Stop exploring and stop "+
			"re-reading files. Apply the smallest change that completes the task now, then reply with a "+
			"plain-text summary of what you changed and what is left. Unfinished work is committed to the "+
			"task branch and picked up by the next run, so a clear summary is more useful than a rushed edit.",
		used, cap)
}

func repeatNudgeMessage(name string, repeats int) string {
	left := max(repeatAbortThreshold-repeats, 1)
	plural := "s"
	if left == 1 {
		plural = ""
	}
	return fmt.Sprintf(
		"[loop guard] You have now made this exact %s call %d times and it returned the same result every time. "+
			"That result is no longer shown to you, and the call is no longer being run — asking again gains nothing.\n"+
			"Empty output is not a failure: sed, mv, cp and mkdir print nothing when they succeed.\n"+
			"Do one of these instead:\n"+
			"1. Read the file or state this call was meant to change, and continue from what you find.\n"+
			"2. Change the method — write the whole file instead of editing it in place, or use a different tool.\n"+
			"3. If neither is possible, stop calling tools and summarise what you changed and what is blocked.\n"+
			"%d more identical call%s and this run is stopped with the task unfinished.",
		name, repeats+1, left, plural)
}

func sameCallMessage(name string, execs int) string {
	left := max(sameCallAbortThreshold-execs, 1)
	plural := "s"
	if left == 1 {
		plural = ""
	}
	return fmt.Sprintf(
		"\n\n[loop guard] You have now run this exact %s call %d times in this run. Its result changes slightly each time "+
			"(a duration, a timestamp, a counter on the page), but nothing about the task has changed with it.\n"+
			"If it passed, you already have your evidence — record it and move the task on. If it failed, the next call must be an "+
			"EDIT that changes the cause; running the same command again cannot change the outcome.\n"+
			"Only call it again after you have changed something it would actually see. "+
			"%d more identical call%s and this run is stopped with the task unfinished.", name, execs, left, plural)
}

func emptyResultNote(name string) string {
	return fmt.Sprintf(
		"[no output] %s ran successfully and returned nothing at all.\n"+
			"That empty result is the tool's answer, not a failure to run: whatever you asked for is not there, "+
			"or the arguments pointed at something that holds nothing.\n"+
			"Repeating this exact call will return the same emptiness. Change the arguments, or use a different tool.", name)
}

func errorStreakMessage(streak int) string {
	left := max(errStreakAbortThreshold-streak, 1)
	plural := "s"
	if left == 1 {
		plural = ""
	}
	return fmt.Sprintf(
		"[loop guard] Your last %d tool calls in a row all failed. Changing only the arguments is not working; "+
			"the method is what is wrong.\n"+
			"Before the next call, do one of these:\n"+
			"1. Read the actual file, directory or command output the failures are about, instead of guessing at paths.\n"+
			"2. Use a different tool for the same goal — write the whole file rather than patching it, list a directory rather than assuming it.\n"+
			"3. If the environment is missing something you need, stop and summarise what is blocked instead of retrying.\n"+
			"%d more consecutive failure%s and this run is stopped with the task unfinished.\n\n"+
			"The failing call's own output follows:\n", streak, left, plural)
}

func toolErrorMessage(name string, count int) string {
	return fmt.Sprintf(
		"\n\n[loop guard] %s has now failed %d times in this run. Whatever you are passing it is not the shape it wants — "+
			"check its description again, or reach the same goal with a different tool.", name, count)
}

const wrapUpPrompt = "Your tool budget for this run is spent. Do not call any more tools. " +
	"Reply with a short plain-text summary: what you changed (files), what works, and what is still missing."

const emptyTurnPrompt = "Your last turn was empty — no text and no tool call. " +
	"An empty turn is not an answer. Reply now, in plain text, to what was asked: " +
	"what you did, what it changed, and what is left. If a tool call is still needed to answer, make it."

const emptyTurnFallback = "The model returned an empty answer twice in a row, so this turn produced no reply. " +
	"Any tool calls made before it did run — check the board records and the actions listed above — but nothing was written back here. " +
	"Send the request again, ideally in smaller steps."
