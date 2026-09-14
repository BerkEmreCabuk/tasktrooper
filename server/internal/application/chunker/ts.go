package chunker

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/makifbaysal/tasktrooper/server/internal/application/treesitter"
)

// TSChunker covers the JavaScript family (.ts/.tsx/.js/.jsx/.mjs/.cjs). Before
// it existed these files went through FallbackChunker, so every chunk was a
// 200-line slice and every "symbol" was the file name — symbol search over a
// React or Node repository returned file names and nothing else.
type TSChunker struct{}

func (TSChunker) Chunk(filePath string, content []byte) ([]Chunk, error) {
	return chunkWithSitter(tsSpec, filePath, content)
}

var tsSpec = sitterSpec{
	language: "typescript",
	parse: func(path string, content []byte) *sitter.Tree {
		return treesitter.ParseJSFamily(path, content)
	},
	decls: map[string]declRule{
		"function_declaration":           {kind: "function"},
		"generator_function_declaration": {kind: "function"},
		"class_declaration":              {kind: "class", container: true},
		"abstract_class_declaration":     {kind: "class", container: true},
		"interface_declaration":          {kind: "interface"},
		"type_alias_declaration":         {kind: "type"},
		"enum_declaration":               {kind: "enum"},
		// Reached by descending through lexical_declaration, so only bindings at
		// module scope match: anything inside a function is already covered by
		// that function's chunk.
		"variable_declarator": {kind: "const"},
	},
	members: map[string]declRule{
		"method_definition":       {kind: "method"},
		"public_field_definition": {kind: "property"},
	},
	unwrap: func(node *sitter.Node) *sitter.Node {
		if node.Type() != "export_statement" {
			return nil
		}
		return node.ChildByFieldName("declaration")
	},
	kindOf: func(node *sitter.Node, content []byte, rule declRule) string {
		if node.Type() != "variable_declarator" && node.Type() != "public_field_definition" {
			return rule.kind
		}
		value := node.ChildByFieldName("value")
		if value == nil {
			return rule.kind
		}
		switch value.Type() {
		case "arrow_function", "function", "function_expression", "generator_function":
			return "function"
		}
		return rule.kind
	},
}
