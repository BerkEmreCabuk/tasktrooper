package graph

import (
	"fmt"
	"go/parser"
	"go/token"
	"strings"
)

func ExtractGoImports(filePath string, content []byte) ([]Edge, error) {
	src := string(content)
	if strings.TrimSpace(src) == "" {
		return nil, nil
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parse go file: %w", err)
	}

	from := SymbolRef{
		FilePath:   filePath,
		SymbolName: filePath,
		Kind:       "file",
	}

	var edges []Edge
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		edges = append(edges, Edge{
			From: from,
			To: SymbolRef{
				FilePath:   path,
				SymbolName: path,
				Kind:       "import",
			},
			Kind: EdgeImport,
		})
	}
	return edges, nil
}
