package chunker

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/makifbaysal/tasktrooper/server/internal/application/treesitter"
)

type PythonChunker struct{}

func (PythonChunker) Chunk(filePath string, content []byte) ([]Chunk, error) {
	return chunkWithSitter(pythonSpec, filePath, content)
}

var pythonSpec = sitterSpec{
	language: "python",
	parse: func(_ string, content []byte) *sitter.Tree {
		return treesitter.ParsePython(content)
	},
	decls: map[string]declRule{
		"function_definition": {kind: "function"},
		"class_definition":    {kind: "class", container: true},
	},
	members: map[string]declRule{
		"function_definition": {kind: "method"},
	},
	// A decorated def is wrapped; classify by the definition but keep the
	// decorator lines inside the chunk, since @app.route carries the meaning.
	unwrap: func(node *sitter.Node) *sitter.Node {
		if node.Type() != "decorated_definition" {
			return nil
		}
		return node.ChildByFieldName("definition")
	},
}
