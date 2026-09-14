package chunker

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/makifbaysal/tasktrooper/server/internal/application/treesitter"
)

type JavaChunker struct{}

func (JavaChunker) Chunk(filePath string, content []byte) ([]Chunk, error) {
	return chunkWithSitter(javaSpec, filePath, content)
}

var javaSpec = sitterSpec{
	language: "java",
	parse: func(_ string, content []byte) *sitter.Tree {
		return treesitter.ParseJava(content)
	},
	decls: map[string]declRule{
		"class_declaration":           {kind: "class", container: true},
		"interface_declaration":       {kind: "interface", container: true},
		"enum_declaration":            {kind: "enum", container: true},
		"record_declaration":          {kind: "record", container: true},
		"annotation_type_declaration": {kind: "interface", container: true},
	},
	// Fields stay with the type header; only the executable members earn their
	// own chunk.
	members: map[string]declRule{
		"method_declaration":      {kind: "method"},
		"constructor_declaration": {kind: "method"},
		"class_declaration":       {kind: "class", container: true},
		"interface_declaration":   {kind: "interface", container: true},
		"enum_declaration":        {kind: "enum", container: true},
	},
}
