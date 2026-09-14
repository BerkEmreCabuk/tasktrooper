package board

import (
	"context"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// duplicateTitleThreshold is the token-overlap ratio above which two titles are
// treated as the same piece of work. 0.7 separates the observed collision
// ("Implement Google Play Store Link on Acme Website" vs "Add Google Play
// Store Link to Acme Website", 0.75) from tasks that merely share a subject
// ("Add Google Play link" vs "Add App Store link", 0.5).
const duplicateTitleThreshold = 0.7

// titleStopWords carry no signal about WHAT the work is, so leaving them in
// would let two unrelated short titles look similar.
var titleStopWords = map[string]bool{
	"a": true, "an": true, "the": true, "to": true, "on": true, "in": true,
	"for": true, "of": true, "and": true, "or": true, "with": true, "at": true,
	"into": true, "from": true, "by": true,
}

// titleTokens normalises a title to a set of meaningful lowercase words.
func titleTokens(title string) map[string]bool {
	fields := strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make(map[string]bool, len(fields))
	for _, f := range fields {
		if !titleStopWords[f] {
			out[f] = true
		}
	}
	return out
}

// titleSimilarity is the Jaccard index of two titles' meaningful tokens.
func titleSimilarity(a, b string) float64 {
	ta, tb := titleTokens(a), titleTokens(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	overlap := 0
	for tok := range ta {
		if tb[tok] {
			overlap++
		}
	}
	union := len(ta) + len(tb) - overlap
	if union == 0 {
		return 0
	}
	return float64(overlap) / float64(union)
}

// isOpenColumn reports whether a task in this column still represents
// outstanding work. A finished or released task must not block a new one that
// happens to be phrased like it.
func isOpenColumn(col domain.TaskColumn) bool {
	switch col {
	case domain.TaskColumnDone, domain.TaskColumnReleased:
		return false
	}
	return true
}

// findDuplicateTask returns an open task on the same repository whose title
// describes the same work as the requested one.
//
// Two sibling subtasks of a single plan run concurrently and each receives the
// conversation history as it looked before the run started, so neither can see
// what the other just wrote to the board. Told to open the same task, both open
// it — with their own phrasing, which is why the duplicates are not identical.
// The ordering guarantee has to come from the board itself.
func (kit *ToolKit) findDuplicateTask(ctx context.Context, repositoryID uuid.UUID, title string) (domain.BoardTask, bool) {
	if kit.Tasks == nil {
		return domain.BoardTask{}, false
	}
	tasks, err := kit.Tasks.ListTasks(ctx, repositoryID)
	if err != nil {
		// A lookup failure must not block task creation — the guard is a
		// safety net, not a gate.
		return domain.BoardTask{}, false
	}
	best := domain.BoardTask{}
	bestScore := 0.0
	for _, t := range tasks {
		if !isOpenColumn(t.Column) {
			continue
		}
		if score := titleSimilarity(t.Title, title); score >= duplicateTitleThreshold && score > bestScore {
			best, bestScore = t, score
		}
	}
	return best, bestScore > 0
}
