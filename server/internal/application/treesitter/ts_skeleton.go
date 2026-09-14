package treesitter

import (
	sitter "github.com/smacker/go-tree-sitter"
)

type TSSymbol struct {
	Kind      string
	Name      string
	Signature string
}

func ExtractTSSkeleton(content []byte, path string) []TSSymbol {
	tree := ParseJSFamily(path, content)
	if tree == nil {
		return nil
	}
	defer tree.Close()

	var symbols []TSSymbol
	collectTSSymbols(tree.RootNode(), content, &symbols)
	return dedupeTSSymbols(symbols)
}

func collectTSSymbols(node *sitter.Node, content []byte, symbols *[]TSSymbol) {
	if node == nil {
		return
	}
	switch node.Type() {
	case "export_statement":
		for i := 0; i < int(node.NamedChildCount()); i++ {
			collectTSExportedDecl(node.NamedChild(i), content, symbols)
		}
		return
	case "function_declaration", "generator_function_declaration":
		collectTSSymbolDecl(node, content, symbols, false)
	case "class_declaration":
		collectTSClassDecl(node, content, symbols, false)
	case "interface_declaration", "type_alias_declaration", "enum_declaration":
		collectTSSymbolDecl(node, content, symbols, false)
	case "lexical_declaration", "variable_declaration":
		collectTSVariableDecl(node, content, symbols, false)
	}

	for i := 0; i < int(node.NamedChildCount()); i++ {
		collectTSSymbols(node.NamedChild(i), content, symbols)
	}
}

func collectTSExportedDecl(node *sitter.Node, content []byte, symbols *[]TSSymbol) {
	if node == nil {
		return
	}
	switch node.Type() {
	case "lexical_declaration", "variable_declaration":
		collectTSVariableDecl(node, content, symbols, true)
	case "class_declaration":
		collectTSClassDecl(node, content, symbols, true)
	default:
		collectTSSymbolDecl(node, content, symbols, true)
	}
}

func collectTSClassDecl(node *sitter.Node, content []byte, symbols *[]TSSymbol, exported bool) {
	collectTSSymbolDecl(node, content, symbols, exported)
	for i := 0; i < int(node.NamedChildCount()); i++ {
		if node.NamedChild(i).Type() == "class_body" {
			collectTSClassMembers(node.NamedChild(i), content, symbols)
		}
	}
}

func collectTSClassMembers(body *sitter.Node, content []byte, symbols *[]TSSymbol) {
	for i := 0; i < int(body.NamedChildCount()); i++ {
		member := body.NamedChild(i)
		if member.Type() != "method_definition" {
			continue
		}
		nameNode := ChildByField(member, "name")
		if nameNode == nil {
			continue
		}
		*symbols = append(*symbols, TSSymbol{
			Kind:      "method",
			Name:      NodeText(nameNode, content),
			Signature: HeaderText(member, content, "body"),
		})
	}
}

func collectTSSymbolDecl(node *sitter.Node, content []byte, symbols *[]TSSymbol, exported bool) {
	nameNode := ChildByField(node, "name")
	if nameNode == nil {
		return
	}
	kind := tsDeclKind(node.Type(), exported)
	sig := ""
	switch node.Type() {
	case "function_declaration", "generator_function_declaration", "method_definition":
		sig = HeaderText(node, content, "body")
	case "class_declaration", "interface_declaration", "type_alias_declaration", "enum_declaration":
		sig = HeaderText(node, content, "body")
	}
	*symbols = append(*symbols, TSSymbol{
		Kind:      kind,
		Name:      NodeText(nameNode, content),
		Signature: sig,
	})
}

func collectTSVariableDecl(node *sitter.Node, content []byte, symbols *[]TSSymbol, exported bool) {
	for i := 0; i < int(node.NamedChildCount()); i++ {
		child := node.NamedChild(i)
		if child.Type() != "variable_declarator" {
			continue
		}
		nameNode := ChildByField(child, "name")
		if nameNode == nil {
			continue
		}
		value := ChildByField(child, "value")
		kind := "const"
		if exported {
			kind = "export"
		}
		if value != nil && (value.Type() == "arrow_function" || value.Type() == "function") {
			kind = "function"
		}
		sig := HeaderText(child, content, "value")
		if sig == "" {
			sig = NodeText(child, content)
		}
		*symbols = append(*symbols, TSSymbol{
			Kind:      kind,
			Name:      NodeText(nameNode, content),
			Signature: sig,
		})
	}
}

func tsDeclKind(nodeType string, exported bool) string {
	switch nodeType {
	case "function_declaration", "generator_function_declaration":
		return "function"
	case "class_declaration":
		return "class"
	case "interface_declaration":
		return "interface"
	case "type_alias_declaration", "enum_declaration":
		if exported {
			return "export"
		}
		return "type"
	default:
		return "symbol"
	}
}

func dedupeTSSymbols(symbols []TSSymbol) []TSSymbol {
	seen := make(map[string]struct{}, len(symbols))
	out := make([]TSSymbol, 0, len(symbols))
	for _, sym := range symbols {
		key := sym.Kind + ":" + sym.Name
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, sym)
	}
	return out
}
