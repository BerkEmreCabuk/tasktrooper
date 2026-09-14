package mapper

import (
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/treesitter"
)

func extractPySkeleton(path string, content []byte) FileSkeleton {
	doc, symbols := treesitter.ExtractPySkeleton(content)
	sk := FileSkeleton{Path: path, Doc: doc}
	for _, sym := range symbols {
		sk.Symbols = append(sk.Symbols, Symbol{
			Kind:      sym.Kind,
			Name:      sym.Name,
			Signature: sym.Signature,
		})
	}
	return sk
}

func isPyPath(path string) bool {
	return strings.HasSuffix(strings.ToLower(path), ".py")
}
