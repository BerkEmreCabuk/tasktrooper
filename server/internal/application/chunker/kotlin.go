package chunker

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/makifbaysal/tasktrooper/server/internal/application/treesitter"
)

type KotlinChunker struct{}

func (KotlinChunker) Chunk(filePath string, content []byte) ([]Chunk, error) {
	return chunkWithSitter(kotlinSpec, filePath, content)
}

// kotlinDeclKeywords maps the keyword that opens a declaration to its chunk
// kind. The grammar files class, interface and data class under one node type,
// so the keyword is the only thing that tells them apart.
var kotlinDeclKeywords = map[string]string{
	"class":     "class",
	"interface": "interface",
	"object":    "object",
	"enum":      "enum",
}

var kotlinSpec = sitterSpec{
	language: "kotlin",
	parse: func(_ string, content []byte) *sitter.Tree {
		return treesitter.ParseKotlin(content)
	},
	decls: map[string]declRule{
		"class_declaration":    {kind: "class", container: true},
		"object_declaration":   {kind: "object", container: true},
		"function_declaration": {kind: "function"},
		"property_declaration": {kind: "property"},
	},
	members: map[string]declRule{
		"function_declaration":  {kind: "method"},
		"property_declaration":  {kind: "property"},
		"secondary_constructor": {kind: "method"},
		"companion_object":      {kind: "object", container: true},
		"class_declaration":     {kind: "class", container: true},
		"object_declaration":    {kind: "object", container: true},
	},
	kindOf: func(node *sitter.Node, content []byte, rule declRule) string {
		if node.Type() != "class_declaration" {
			return rule.kind
		}
		if kind := leadingKeyword(node, content, kotlinDeclKeywords); kind != "" {
			return kind
		}
		return rule.kind
	},
}
