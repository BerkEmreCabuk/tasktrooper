package graph

import (
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/application/treesitter"
)

// HasGrammarGraph reports whether a file's language can produce import and call
// edges from its grammar. Go is handled by go/ast, not here.
func HasGrammarGraph(filePath string) bool {
	return treesitter.GraphSpecFor(filePath)
}

// ExtractImports returns one import edge per module a file pulls in. The target
// is the module as written — a relative path in TypeScript, a package in Java
// or Kotlin, a framework in Swift — which is what the graph stores for Go and
// TypeScript already.
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

// BuildCallGraphInRange returns the calls one symbol makes, scoped by the line
// range its chunk covers. The range comes from the chunker, so a method chunk
// named Class.method resolves to exactly its own body — a name lookup would
// miss it, since the grammar only knows the member as `method`.
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
