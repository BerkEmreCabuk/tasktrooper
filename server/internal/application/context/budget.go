package context

import (
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type Budget struct {
	MaxTokens          int
	ReserveOutput      int
	SummarizeThreshold int
	KeepRecentMessages int
}

func (b Budget) Apply(messages []domain.Message) []domain.Message {
	limit := b.TokenLimit()
	if CountTokens(messages) <= limit {
		out := make([]domain.Message, len(messages))
		copy(out, messages)
		return out
	}

	working := make([]domain.Message, len(messages))
	copy(working, messages)

	for CountTokens(working) > limit {
		if idx, ok := b.findRemovableTool(working); ok {
			working = append(working[:idx], working[idx+1:]...)
			continue
		}
		if idx, ok := b.findRemovableMessage(working); ok {
			working = append(working[:idx], working[idx+1:]...)
			continue
		}
		// Nothing may be removed whole any more — but a protected turn can
		// still be over the limit purely on pictures, and one image is worth
		// hundreds of tokens. Shedding the oldest image keeps the words the
		// user wrote, which is the part the model cannot guess.
		if dropOldestImage(working) {
			continue
		}
		break
	}
	return working
}

// dropOldestImage sheds the single oldest image in the history — oldest message
// first, first image within that message — and reports whether it found one.
// The message itself stays, text intact, even once it has no images left.
//
// The Images slice is cloned before the mutation: Apply's messages are a
// shallow copy, so writing through the shared backing array would strip images
// out of the caller's own history for good.
func dropOldestImage(messages []domain.Message) bool {
	for i := range messages {
		if len(messages[i].Images) == 0 {
			continue
		}
		remaining := make([]domain.ToolResultImage, 0, len(messages[i].Images)-1)
		remaining = append(remaining, messages[i].Images[1:]...)
		messages[i].Images = remaining
		return true
	}
	return false
}

// TokenLimit is the largest history, in tokens, that may be sent: the whole
// window minus the room the answer needs. Exported because callers that trim
// before handing a history over need to ask the same question Apply asks, and
// two copies of the arithmetic are two copies that can drift.
func (b Budget) TokenLimit() int {
	limit := b.MaxTokens - b.ReserveOutput
	if limit < 1 {
		return 1
	}
	return limit
}

func (b Budget) findRemovableTool(messages []domain.Message) (int, bool) {
	lastUser := lastUserIndex(messages)
	recent := recentIndexSet(len(messages), b.KeepRecentMessages)
	for i, m := range messages {
		if m.Role != domain.RoleTool {
			continue
		}
		if i == lastUser {
			continue
		}
		if _, ok := recent[i]; ok {
			continue
		}
		return i, true
	}
	return 0, false
}

func (b Budget) findRemovableMessage(messages []domain.Message) (int, bool) {
	lastUser := lastUserIndex(messages)
	recent := recentIndexSet(len(messages), b.KeepRecentMessages)
	for i, m := range messages {
		if m.Role == domain.RoleSystem {
			continue
		}
		if i == lastUser {
			continue
		}
		if _, ok := recent[i]; ok {
			continue
		}
		return i, true
	}
	return 0, false
}

func lastUserIndex(messages []domain.Message) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == domain.RoleUser {
			return i
		}
	}
	return -1
}

func recentIndexSet(n, keepRecent int) map[int]struct{} {
	if keepRecent <= 0 || n == 0 {
		return map[int]struct{}{}
	}
	start := 0
	if n > keepRecent {
		start = n - keepRecent
	}
	set := make(map[int]struct{}, n-start)
	for i := start; i < n; i++ {
		set[i] = struct{}{}
	}
	return set
}
