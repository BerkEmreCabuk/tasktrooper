package context

import (
	gocontext "context"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// SummaryMarker opens the one message that stands in for everything a run has
// dropped so far.
//
// domain.Message carries no metadata, so the marker IS the block's identity: it
// is how the next trim finds the block it must REPLACE (instead of adding a
// second one, and a third), and how anyone reading the transcript can tell a
// summary apart from something the model actually said. Changing it between
// releases costs one degraded summary per in-flight run — an unrecognised old
// block is summarized into the new one rather than replaced — so it is written
// to be stable, not pretty.
const SummaryMarker = "[earlier turns of this run, summarised]"

// maxSummaryChars bounds the block itself. Without a ceiling the one message
// that is supposed to make room could be handed back as several thousand tokens
// of prose, and the next trim would have to start by dropping its own summary.
const maxSummaryChars = 4000

// summaryReserveTokens is what the planner assumes the new block will cost
// before it exists. It is the ceiling above, not a guess at the model's
// brevity: planning the cut against an optimistic number is how a trim lands
// back over the limit and trims again on the very next turn.
const summaryReserveTokens = maxSummaryChars / charsPerToken

// maxSummaryInputTokens caps what a single summarize call is fed. The call is a
// real LLM round-trip billed to the run, and a span far larger than this is
// past the point where one paragraph could represent it anyway. Overflow is cut
// from the OLDEST end — content that is already the least likely to matter — so
// the newest dropped turns are always the ones that survive into the summary.
const maxSummaryInputTokens = 24000

// StableTrimOptions describes the run whose history is being cut.
type StableTrimOptions struct {
	// HeadLen is how many leading messages are the run's opening context: the
	// persona, the workspace and project blocks, the trigger, the evidence
	// gathered before the first turn. The caller records it as len(history) at
	// run start rather than having the trimmer guess by role, because only the
	// caller knows where its own assembly stopped and the model's turns began.
	//
	// Nothing inside the head is ever dropped or reordered. That is the entire
	// point of this trim: those bytes are the shared prefix every provider
	// prefix cache (Anthropic cache_control, OpenAI/Gemini automatic caching,
	// llama.cpp's KV cache) keys on, and the moment one of them moves, every
	// later token has to be re-processed for the rest of the run.
	HeadLen int
	// Model and Provider are the run's own, so the summary is billed and routed
	// exactly like the turns it is condensing.
	Model    string
	Provider domain.LLMProviderType
}

// StableTrim cuts a history down to the budget WITHOUT disturbing the bytes in
// front of the cut.
//
// Budget.Apply, the trim this replaces, removes the oldest removable message
// from the MIDDLE of the history. Every removal shifts everything after it, so
// the provider's cached prefix stops matching from that point on — and an
// eighty-iteration board run trims on nearly every turn, which means the most
// expensive runs in the system got no cache benefit at all.
//
// What happens here instead: everything between the protected head and the
// protected recent tail is replaced by ONE summary message at a FIXED position
// (immediately after the head). The head keeps its bytes, the summary block
// keeps its index, and only the region after it changes. A later trim replaces
// that same block in place, feeding the old summary back in so nothing is
// silently lost.
//
// The cut also runs on hysteresis: it triggers at the budget ceiling but cuts
// down to Budget's lower watermark, so trims come in batches with many
// byte-stable iterations between them instead of one trim per turn.
//
// Returns ok=false when a stable trim is not possible (no summarizer, already
// under budget, nothing droppable, empty summary) — the caller falls back to
// Budget.Apply. A non-nil error means the summarize call itself failed; that is
// the same fallback, but worth logging.
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
		// The whole history is head. Nothing here may be touched, so the caller's
		// fallback — which at least sheds images — is the only thing left.
		return nil, false, nil
	}

	// A block already at the anchor is not dropped and forgotten: it is fed back
	// to the summarizer alongside the newly dropped span, so the second summary
	// covers everything the first one did plus what happened since.
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

// SummaryBlockIndex reports where the run summary sits, or -1 when the history
// has none. Exported for callers that need to reason about the block they can
// see (tests, diagnostics) without re-deriving the marker rule.
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

// summaryAnchor is the fixed index the summary block occupies: the end of the
// head, pushed past anything that must stay glued to it.
//
// The push exists for one case. A tool result whose assistant tool-call turn is
// not in the request is rejected by every provider, and Anthropic wants the
// results adjacent to the call. If a caller's head ends on an assistant turn
// that asked for tools, the results answering it belong to the head too — so
// the anchor steps over them rather than landing between the two.
//
// It is deterministic given HeadLen, and stable once a summary exists: the
// summary is a system message, so the scan stops on it and returns the same
// index it returned last time.
func summaryAnchor(messages []domain.Message, headLen int) int {
	at := min(max(headLen, 0), len(messages))
	for at < len(messages) && messages[at].Role == domain.RoleTool {
		at++
	}
	return at
}

// chooseDropEnd picks how far the cut reaches: far enough to land under the
// LOWER watermark, and no further.
//
// Two properties are being bought. Dropping only as much as the watermark needs
// keeps the newest turns that still fit — the run's most useful context — out
// of the summary. Cutting to the watermark rather than to the ceiling leaves
// real headroom, so the next several iterations append to a request whose
// prefix has not moved at all. Cutting to just under the ceiling would trim
// again on the very next turn, which is the behaviour this whole file exists to
// remove.
func (b Budget) chooseDropEnd(messages []domain.Message, anchor, dropStart, limit int) int {
	maxDropEnd := b.protectedTailStart(messages, dropStart)
	if maxDropEnd <= dropStart {
		return dropStart
	}
	target := b.trimTarget(limit)

	headChars, headImages := spanCost(messages[:anchor])
	suffixChars, suffixImages := suffixCost(messages)
	// What the request would cost if the cut ended here: the untouched head, the
	// summary block that replaces the span, and everything from dropEnd on.
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
	// Take what there is; the caller checks the ceiling afterwards and still has
	// Budget.Apply (and its image shedding) for what is left.
	return alignDropEnd(messages, maxDropEnd)
}

// alignDropEnd never lets a cut end in the middle of a tool exchange. If the
// first kept message is a tool result, its assistant turn was just dropped and
// the result is now an orphan the provider will reject — so it goes too.
func alignDropEnd(messages []domain.Message, dropEnd int) int {
	for dropEnd < len(messages) && messages[dropEnd].Role == domain.RoleTool {
		dropEnd++
	}
	return dropEnd
}

// protectedTailStart is where the verbatim recent tail begins: KeepRecent
// messages from the end, walked back off any tool result so the tail never
// opens on a call it cannot show.
func (b Budget) protectedTailStart(messages []domain.Message, lowerBound int) int {
	keep := max(b.KeepRecentMessages, 0)
	start := min(max(len(messages)-keep, lowerBound), len(messages))
	for start > lowerBound && start < len(messages) && messages[start].Role == domain.RoleTool {
		start--
	}
	return start
}

// trimTarget is the lower watermark a trim cuts down to. SummarizeThreshold is
// the configured one (16000 against a 27904-token ceiling); a config that never
// set it, or set it above the ceiling where it could never fire, gets three
// quarters of the ceiling so the hysteresis still exists.
func (b Budget) trimTarget(limit int) int {
	target := b.SummarizeThreshold
	if target <= 0 || target >= limit {
		target = limit * 3 / 4
	}
	return max(target, 1)
}

// capSummaryInput bounds one summarize call's input, dropping from the oldest
// end. The carried summary is pinned: it represents everything already dropped
// in this run, and losing it would lose all of it at once.
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

// suffixCost precomputes the cost of every suffix, so choosing the cut is a
// scan with O(1) lookups instead of re-counting the whole history per candidate.
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
