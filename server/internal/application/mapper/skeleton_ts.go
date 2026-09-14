package mapper

import (
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/treesitter"
)

func extractTSSkeleton(path string, content []byte) FileSkeleton {
	sk := FileSkeleton{Path: path}
	for _, sym := range treesitter.ExtractTSSkeleton(content, path) {
		sk.Symbols = append(sk.Symbols, Symbol{
			Kind:      sym.Kind,
			Name:      sym.Name,
			Signature: sym.Signature,
		})
	}
	return sk
}

func isTSPath(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".ts") ||
		strings.HasSuffix(lower, ".tsx") ||
		strings.HasSuffix(lower, ".js") ||
		strings.HasSuffix(lower, ".jsx") ||
		strings.HasSuffix(lower, ".mjs") ||
		strings.HasSuffix(lower, ".cjs")
}

func dedupeSymbols(symbols []Symbol) []Symbol {
	seen := make(map[string]struct{}, len(symbols))
	out := make([]Symbol, 0, len(symbols))
	for _, sym := range symbols {
		key := sym.Kind + ":" + sym.Name
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, sym)
	}
	return out
}
