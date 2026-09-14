package treesitter

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

type PySymbol struct {
	Kind      string
	Name      string
	Signature string
}

func ExtractPySkeleton(content []byte) (string, []PySymbol) {
	tree := ParsePython(content)
	if tree == nil {
		return "", nil
	}
	defer tree.Close()

	root := tree.RootNode()
	doc := extractPyModuleDoc(root, content)
	var symbols []PySymbol
	collectPySymbols(root, content, &symbols)
	return doc, dedupePySymbols(symbols)
}

func extractPyModuleDoc(root *sitter.Node, content []byte) string {
	if root == nil || root.NamedChildCount() == 0 {
		return ""
	}
	first := root.NamedChild(0)
	if first.Type() != "expression_statement" {
		return ""
	}
	if first.NamedChildCount() == 0 {
		return ""
	}
	expr := first.NamedChild(0)
	if expr.Type() != "string" {
		return ""
	}
	return strings.TrimSpace(UnquoteString(NodeText(expr, content)))
}

func collectPySymbols(node *sitter.Node, content []byte, symbols *[]PySymbol) {
	if node == nil {
		return
	}
	switch node.Type() {
	case "function_definition":
		nameNode := ChildByField(node, "name")
		if nameNode != nil {
			*symbols = append(*symbols, PySymbol{
				Kind:      "function",
				Name:      NodeText(nameNode, content),
				Signature: HeaderText(node, content, "body"),
			})
		}
	case "class_definition":
		nameNode := ChildByField(node, "name")
		if nameNode != nil {
			*symbols = append(*symbols, PySymbol{
				Kind:      "class",
				Name:      NodeText(nameNode, content),
				Signature: HeaderText(node, content, "body"),
			})
		}
	}

	for i := 0; i < int(node.NamedChildCount()); i++ {
		collectPySymbols(node.NamedChild(i), content, symbols)
	}
}

func dedupePySymbols(symbols []PySymbol) []PySymbol {
	seen := make(map[string]struct{}, len(symbols))
	out := make([]PySymbol, 0, len(symbols))
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
