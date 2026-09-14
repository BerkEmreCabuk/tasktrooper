package chunker

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/makifbaysal/tasktrooper/server/internal/application/treesitter"
)

type SwiftChunker struct{}

func (SwiftChunker) Chunk(filePath string, content []byte) ([]Chunk, error) {
	return chunkWithSitter(swiftSpec, filePath, content)
}

// swiftDeclKeywords disambiguates the grammar's single class_declaration node,
// which stands in for class, struct, enum, extension and actor alike.
var swiftDeclKeywords = map[string]string{
	"class":     "class",
	"struct":    "struct",
	"enum":      "enum",
	"extension": "extension",
	"actor":     "actor",
}

// swiftUnnamedDecls are the members Swift spells with a keyword instead of a
// name; without this they would inherit the first parameter's name.
var swiftUnnamedDecls = map[string]string{
	"init_declaration":      "init",
	"deinit_declaration":    "deinit",
	"subscript_declaration": "subscript",
}

var swiftSpec = sitterSpec{
	language: "swift",
	parse: func(_ string, content []byte) *sitter.Tree {
		return treesitter.ParseSwift(content)
	},
	decls: map[string]declRule{
		"class_declaration":     {kind: "class", container: true},
		"protocol_declaration":  {kind: "protocol", container: true},
		"function_declaration":  {kind: "function"},
		"property_declaration":  {kind: "property"},
		"typealias_declaration": {kind: "type"},
	},
	members: map[string]declRule{
		"function_declaration":          {kind: "method"},
		"init_declaration":              {kind: "method"},
		"deinit_declaration":            {kind: "method"},
		"subscript_declaration":         {kind: "method"},
		"property_declaration":          {kind: "property"},
		"protocol_function_declaration": {kind: "method"},
		"protocol_property_declaration": {kind: "property"},
		"class_declaration":             {kind: "class", container: true},
	},
	nameOf: func(node *sitter.Node, _ []byte) string {
		return swiftUnnamedDecls[node.Type()]
	},
	kindOf: func(node *sitter.Node, content []byte, rule declRule) string {
		if node.Type() != "class_declaration" {
			return rule.kind
		}
		if kind := leadingKeyword(node, content, swiftDeclKeywords); kind != "" {
			return kind
		}
		return rule.kind
	},
}
