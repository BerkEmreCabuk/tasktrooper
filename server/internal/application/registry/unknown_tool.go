package registry

import (
	"sort"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/prompt"
)

type unknownToolData struct {
	Called     string
	Available  []string
	Suggestion string
}

var unknownToolKey = prompt.Define("registry.unknown_tool", unknownToolData{
	Called:     "example_tool",
	Available:  []string{"list_board_tasks"},
	Suggestion: "list_board_tasks",
})

// unknownToolMessage answers a call to a tool that does not exist with the tools
// that do. A model that invents a name (delete_lines, apply_patch) will
// otherwise guess again from memory, and the second guess is often a real tool
// with the wrong blast radius — delete_file for "remove these lines". Naming the
// nearest real tool, and listing the rest, turns a dead turn into a corrected
// one. Wording lives in catalog/system/prompts/registry/unknown_tool.md.
func unknownToolMessage(called string, available []string) string {
	names := append([]string(nil), available...)
	sort.Strings(names)

	suggestion := ""
	if len(names) > 0 {
		suggestion = nearestToolName(called, names)
	}
	return unknownToolKey.Render(unknownToolData{
		Called:     called,
		Available:  names,
		Suggestion: suggestion,
	})
}

// nearestToolName picks the closest available name, or nothing when the guess
// resembles none of them. A wrong suggestion costs a turn just like no
// suggestion, so the bar is a real overlap: a shared word, or a small edit
// distance relative to the name's length.
//
// What the invented name says about intent lives in its object, not its verb:
// delete_lines is closer to edit_lines than to delete_file, and suggesting the
// latter is how a "remove this function" turn becomes a deleted file. So the
// last word of a name weighs more than the rest, and the edit-distance term is
// capped below one word so it only ever breaks ties.
func nearestToolName(called string, available []string) string {
	called = strings.ToLower(strings.TrimSpace(called))
	if called == "" {
		return ""
	}
	calledWords := nameWords(called)
	if len(calledWords) == 0 {
		return ""
	}
	calledObject := calledWords[len(calledWords)-1]

	best, bestScore := "", 0.0
	for _, name := range available {
		lower := strings.ToLower(name)
		words := nameWords(lower)
		if len(words) == 0 {
			continue
		}

		score := 0.0
		for _, w := range words {
			if !contains(calledWords, w) {
				continue
			}
			if w == calledObject && w == words[len(words)-1] {
				score += 2.0
			} else {
				score += 1.0
			}
		}

		distance := levenshtein(called, lower)
		longest := len(called)
		if len(lower) > longest {
			longest = len(lower)
		}
		if longest > 0 && distance*3 <= longest {
			score += 0.5 * (1.0 - float64(distance)/float64(longest))
		}

		if score > bestScore {
			best, bestScore = name, score
		}
	}
	return best
}

// nameWords splits a tool name into its meaningful parts, in order. Parts of
// one or two letters carry no intent and only create false matches.
func nameWords(name string) []string {
	var words []string
	for _, w := range strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == '-' || r == '.' }) {
		if len(w) > 2 {
			words = append(words, w)
		}
	}
	return words
}

func contains(words []string, target string) bool {
	for _, w := range words {
		if w == target {
			return true
		}
	}
	return false
}

func levenshtein(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, min(curr[j-1]+1, prev[j-1]+cost))
		}
		prev, curr = curr, prev
	}
	return prev[len(br)]
}
