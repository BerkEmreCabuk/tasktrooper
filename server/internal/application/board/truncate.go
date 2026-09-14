package board

import "github.com/makifbaysal/tasktrooper/server/internal/domain"

// The board's strings — run summaries, agent output, git diffs, comments — are
// LLM text written straight to Postgres TEXT columns, so they must never be cut
// mid-rune. domain owns that rule and why it exists; these are the board's names
// for it, kept so the call sites here read as they always have.

func truncateHead(s string, maxBytes int) string { return domain.TruncateHead(s, maxBytes) }

func truncateTail(s string, maxBytes int) string { return domain.TruncateTail(s, maxBytes) }
