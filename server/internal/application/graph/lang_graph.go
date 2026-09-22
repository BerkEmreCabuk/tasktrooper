package graph

import (
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/treesitter"
)

func HasGrammarGraph(filePath string) bool {
	return treesitter.GraphSpecFor(filePath)
}

func ExtractImports(filePath string, content []byte) []Edge {
	if strings.TrimSpace(string(content)) == "" {
		return nil
	}
	from := SymbolRef{FilePath: filePath, SymbolName: filePath, Kind: "file"}

	var edges []Edge
	for _, path := range treesitter.ExtractImportPaths(filePath, content) {
		edges = append(edges, Edge{
			From: from,
			To:   SymbolRef{FilePath: path, SymbolName: path, Kind: "import"},
			Kind: EdgeImport,
		})
	}
	return edges
}

func BuildCallGraphInRange(filePath, symbolName string, content []byte, startLine, endLine int) []Edge {
	if strings.TrimSpace(string(content)) == "" || symbolName == "" {
		return nil
	}
	from := SymbolRef{FilePath: filePath, SymbolName: symbolName, Kind: "function"}

	var edges []Edge
	for _, call := range treesitter.ExtractCallsInRange(filePath, content, startLine, endLine) {
		edges = append(edges, Edge{
			From: from,
			To:   SymbolRef{FilePath: filePath, SymbolName: call.Name, Kind: call.Kind},
			Kind: EdgeCall,
		})
	}
	return edges
}
