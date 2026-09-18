package chunker

import (
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/makifbaysal/tasktrooper/server/internal/application/treesitter"
)

type CSharpChunker struct{}

func (CSharpChunker) Chunk(filePath string, content []byte) ([]Chunk, error) {
	return chunkWithSitter(csharpSpec, filePath, content)
}

// Namespaces are not declarations here: the walk descends through both the
// block and the file-scoped form, so a type is named Orders, not
// Shop.Api.Orders — the same way Java's package never prefixes a class.
var csharpSpec = sitterSpec{
	language: "csharp",
	parse: func(_ string, content []byte) *sitter.Tree {
		return treesitter.ParseCSharp(content)
	},
	decls: map[string]declRule{
		"class_declaration":         {kind: "class", container: true},
		"struct_declaration":        {kind: "struct", container: true},
		"record_declaration":        {kind: "record", container: true},
		"record_struct_declaration": {kind: "record", container: true},
		"interface_declaration":     {kind: "interface", container: true},
		"enum_declaration":          {kind: "enum"},
		"delegate_declaration":      {kind: "type"},
		"local_function_statement":  {kind: "function"},
		"method_declaration":        {kind: "function"},
	},
	// Fields and properties stay with the type header, as Java's fields do: a
	// 30-property DTO would otherwise embed as 30 one-line chunks.
	members: map[string]declRule{
		"method_declaration":        {kind: "method"},
		"constructor_declaration":   {kind: "method"},
		"destructor_declaration":    {kind: "method"},
		"operator_declaration":      {kind: "method"},
		"class_declaration":         {kind: "class", container: true},
		"struct_declaration":        {kind: "struct", container: true},
		"record_declaration":        {kind: "record", container: true},
		"record_struct_declaration": {kind: "record", container: true},
		"interface_declaration":     {kind: "interface", container: true},
		"enum_declaration":          {kind: "enum"},
	},
}
