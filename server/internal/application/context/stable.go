package context

import (
	gocontext "context"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// SummaryMarker is the identity of the block that stands in for everything dropped so far.
//
// domain.Message carries no metadata, so this marker is how the next trim finds the block to replace and how readers tell a summary from model text.
const SummaryMarker = "[earlier turns of this run, summarised]"

// Without a ceiling, the one message meant to make room could return as thousands of tokens.
const maxSummaryChars = 4000

// The planner assumes the new block will cost this much before it exists; optimistic planning trims again next turn.
const summaryReserveTokens = maxSummaryChars / charsPerToken

// A real LLM round-trip; overflow is cut from the oldest end so the newest dropped turns survive.
const maxSummaryInputTokens = 24000

// StableTrimOptions describes the run whose history is being cut.
type StableTrimOptions struct {
	// The run's opening context: persona, workspace and project blocks, trigger, evidence. Recorded as len(history) at run start; nothing inside is ever dropped.
	//
	// It is the shared prefix every provider prefix cache keys on — the moment one of these bytes moves, the cache misses for the rest of the run.
	HeadLen int
	// The run's own, so the summary is billed and routed like the turns it condenses.
	Model    string
	Provider domain.LLMProviderType
}

// StableTrim cuts a history down to budget without disturbing the bytes in front of the cut.
//
// Budget.Apply removes from the middle, shifting everything after it and invalidating the provider cache prefix on nearly every board-run trim.
//
// Here the region between the protected head and the protected tail is replaced by ONE summary message at a FIXED position after the head; a later trim feeds the old summary back in so nothing is silently lost. The cut triggers at the ceiling but lands at the lower watermark, so trims come in batches.
//
// ok=false when a stable trim is not possible — the caller falls back to Budget.Apply. A non-nil error means the summarize call itself failed.
func StableTrim(
	ctx gocontext.Context,
	b Budget,
	s Summarizer,
	messages []domain.Message,
	opts StableTrimOptions,
) ([]domain.Message, bool, error) {
	if s == nil || len(messages) == 0 {
		return nil, false, nil
	}
	limit := b.TokenLimit()
	if CountTokens(messages) <= limit {
		return nil, false, nil
	}

	anchor := summaryAnchor(messages, opts.HeadLen)
	if anchor >= len(messages) {
		// The whole history is head; the caller's fallback is all that is left.
		return nil, false, nil
	}

	// A block already at the anchor is fed back to the summarizer alongside the newly dropped span.
	var carried []domain.Message
	dropStart := anchor
	if isSummaryBlock(messages[anchor]) {
		carried = []domain.Message{messages[anchor]}
		dropStart = anchor + 1
	}

	dropEnd := b.chooseDropEnd(messages, anchor, dropStart, limit)
	if dropEnd <= dropStart {
		return nil, false, nil
	}

	input := capSummaryInput(carried, messages[dropStart:dropEnd], maxSummaryInputTokens)
	summary, err := summarizeWith(ctx, s, input, opts.Model, opts.Provider)
	if err != nil {
		return nil, false, err
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return nil, false, nil
	}

	out := make([]domain.Message, 0, anchor+1+len(messages)-dropEnd)
	out = append(out, messages[:anchor]...)
	out = append(out, domain.Message{
		Role:    domain.RoleSystem,
		Content: SummaryMarker + "\n" + domain.TruncateHead(summary, maxSummaryChars),
	})
	out = append(out, messages[dropEnd:]...)
	return out, true, nil
}

// SummaryBlockIndex reports where the run summary sits, or -1 when the history has none.
func SummaryBlockIndex(messages []domain.Message) int {
	for i, m := range messages {
		if isSummaryBlock(m) {
			return i
		}
	}
	return -1
}

func isSummaryBlock(m domain.Message) bool {
	return m.Role == domain.RoleSystem && strings.HasPrefix(m.Content, SummaryMarker)
}

// summaryAnchor is the summary block's fixed index: the end of the head, stepped over trailing tool results because providers reject a tool result whose assistant call was dropped.
func summaryAnchor(messages []domain.Message, headLen int) int {
	at := min(max(headLen, 0), len(messages))
	for at < len(messages) && messages[at].Role == domain.RoleTool {
		at++
	}
	return at
}

// chooseDropEnd cuts far enough to land under the lower watermark and no further, keeping the newest turns that fit and leaving headroom so later trims do not fire per turn.
func (b Budget) chooseDropEnd(messages []domain.Message, anchor, dropStart, limit int) int {
	maxDropEnd := b.protectedTailStart(messages, dropStart)
	if maxDropEnd <= dropStart {
		return dropStart
	}
	target := b.trimTarget(limit)

	headChars, headImages := spanCost(messages[:anchor])
	suffixChars, suffixImages := suffixCost(messages)
	kept := func(dropEnd int) int {
		chars := headChars + suffixChars[dropEnd]
		return ceilDiv(chars, charsPerToken) + headImages + suffixImages[dropEnd] + summaryReserveTokens
	}

	for dropEnd := dropStart + 1; dropEnd < maxDropEnd; dropEnd++ {
		if kept(dropEnd) <= target {
			return alignDropEnd(messages, dropEnd)
		}
	}
	// Even dropping everything droppable leaves the request over the watermark.
	return alignDropEnd(messages, maxDropEnd)
}

// The cut never ends in the middle of a tool exchange: a first-kept tool result whose assistant turn was dropped is an orphan the provider rejects.
func alignDropEnd(messages []domain.Message, dropEnd int) int {
	for dropEnd < len(messages) && messages[dropEnd].Role == domain.RoleTool {
		dropEnd++
	}
	return dropEnd
}

// protectedTailStart begins KeepRecent messages from the end, walked back off tool results so the tail never opens on an orphan call.
func (b Budget) protectedTailStart(messages []domain.Message, lowerBound int) int {
	keep := max(b.KeepRecentMessages, 0)
	start := min(max(len(messages)-keep, lowerBound), len(messages))
	for start > lowerBound && start < len(messages) && messages[start].Role == domain.RoleTool {
		start--
	}
	return start
}

// trimTarget is the lower watermark: SummarizeThreshold, or three quarters of the ceiling when unset or above it.
func (b Budget) trimTarget(limit int) int {
	target := b.SummarizeThreshold
	if target <= 0 || target >= limit {
		target = limit * 3 / 4
	}
	return max(target, 1)
}

// capSummaryInput bounds one summarize call, dropping from the oldest end; the carried summary is pinned because losing it loses everything already dropped.
func capSummaryInput(carried, span []domain.Message, limit int) []domain.Message {
	room := max(limit-CountTokens(carried), 0)
	start := 0
	for start < len(span) && CountTokens(span[start:]) > room {
		start++
	}
	out := make([]domain.Message, 0, len(carried)+len(span)-start)
	out = append(out, carried...)
	out = append(out, span[start:]...)
	return out
}

func spanCost(messages []domain.Message) (chars, images int) {
	for _, m := range messages {
		chars += messageChars(m)
		images += messageImageTokens(m)
	}
	return chars, images
}

// suffixCost precomputes every suffix's cost so choosing the cut is O(1) lookups instead of re-counting per candidate.
func suffixCost(messages []domain.Message) (chars, images []int) {
	chars = make([]int, len(messages)+1)
	images = make([]int, len(messages)+1)
	for i := len(messages) - 1; i >= 0; i-- {
		chars[i] = chars[i+1] + messageChars(messages[i])
		images[i] = images[i+1] + messageImageTokens(messages[i])
	}
	return chars, images
}

func ceilDiv(a, b int) int {
	if b <= 0 {
		return a
	}
	return (a + b - 1) / b
}
