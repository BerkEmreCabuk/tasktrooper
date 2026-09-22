package board

// Board strings are LLM text in TEXT columns: never cut mid-rune.
import "github.com/makifbaysal/tasktrooper/server/internal/domain"

func truncateHead(s string, maxBytes int) string { return domain.TruncateHead(s, maxBytes) }

func truncateTail(s string, maxBytes int) string { return domain.TruncateTail(s, maxBytes) }
