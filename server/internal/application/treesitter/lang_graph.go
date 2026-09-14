package treesitter

import (
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// SymbolCall is one call site found inside a symbol's body.
type SymbolCall struct {
	Name string
	Kind string
}

// graphSpec adapts a grammar to import/call extraction. Every language the
// indexer chunks has one, so the dependency graph is no longer Go-and-
// TypeScript-only.
type graphSpec struct {
	parse func(path string, content []byte) *sitter.Tree
	// importNodes are the statement types that pull in another module, mapped
	// to the extractor that reads their target(s).
	importNodes map[string]func(node *sitter.Node, content []byte) []string
	// callNodes are the expression types that invoke something, mapped to the
	// extractor that names the callee.
	callNodes map[string]func(node *sitter.Node, content []byte) (SymbolCall, bool)
}

// GraphSpecFor reports whether a path has grammar-backed import/call
// extraction, which is what the indexer switches on.
func GraphSpecFor(path string) bool {
	_, ok := specForPath(path)
	return ok
}

func specForPath(path string) (graphSpec, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs":
		return jsGraphSpec, true
	case ".py":
		return pyGraphSpec, true
	case ".java":
		return javaGraphSpec, true
	case ".kt", ".kts":
		return kotlinGraphSpec, true
	case ".swift":
		return swiftGraphSpec, true
	}
	return graphSpec{}, false
}

// ExtractImportPaths returns the modules a file imports, in source order,
// deduped. It returns nil for a language without a grammar here.
func ExtractImportPaths(path string, content []byte) []string {
	spec, ok := specForPath(path)
	if !ok || strings.TrimSpace(string(content)) == "" {
		return nil
	}
	tree := spec.parse(path, content)
	if tree == nil {
		return nil
	}
	defer tree.Close()

	seen := make(map[string]struct{})
	var paths []string
	WalkNamed(tree.RootNode(), func(node *sitter.Node) {
		extract, ok := spec.importNodes[node.Type()]
		if !ok {
			return
		}
		for _, target := range extract(node, content) {
			target = strings.TrimSpace(target)
			if target == "" {
				continue
			}
			if _, dup := seen[target]; dup {
				continue
			}
			seen[target] = struct{}{}
			paths = append(paths, target)
		}
	})
	return paths
}

// ExtractCallsInRange returns the calls made between startLine and endLine
// (1-based, inclusive). Scoping by line range rather than by looking the symbol
// up again keeps this working for members, overloads and anonymous functions,
// where a name lookup finds the wrong body or none at all.
func ExtractCallsInRange(path string, content []byte, startLine, endLine int) []SymbolCall {
	spec, ok := specForPath(path)
	if !ok || strings.TrimSpace(string(content)) == "" || endLine < startLine {
		return nil
	}
	tree := spec.parse(path, content)
	if tree == nil {
		return nil
	}
	defer tree.Close()

	seen := make(map[string]struct{})
	var calls []SymbolCall
	WalkNamed(tree.RootNode(), func(node *sitter.Node) {
		extract, ok := spec.callNodes[node.Type()]
		if !ok {
			return
		}
		line := int(node.StartPoint().Row) + 1
		if line < startLine || line > endLine {
			return
		}
		call, ok := extract(node, content)
		if !ok || call.Name == "" {
			return
		}
		key := call.Kind + ":" + call.Name
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		calls = append(calls, call)
	})
	return calls
}

// receiverCall reads a Kotlin/Swift call: a bare identifier is a function, a
// navigation expression (`api.get(...)`) is a method on whatever precedes it.
func receiverCall(node *sitter.Node, content []byte) (SymbolCall, bool) {
	if node.NamedChildCount() == 0 {
		return SymbolCall{}, false
	}
	callee := node.NamedChild(0)
	switch callee.Type() {
	case "simple_identifier":
		return SymbolCall{Name: NodeText(callee, content), Kind: "function"}, true
	case "navigation_expression":
		for i := 0; i < int(callee.NamedChildCount()); i++ {
			child := callee.NamedChild(i)
			if child.Type() != "navigation_suffix" {
				continue
			}
			if name := firstNamedOfType(child, "simple_identifier", content); name != "" {
				return SymbolCall{Name: name, Kind: "method"}, true
			}
		}
	}
	return SymbolCall{}, false
}

func firstNamedOfType(node *sitter.Node, nodeType string, content []byte) string {
	for i := 0; i < int(node.NamedChildCount()); i++ {
		if node.NamedChild(i).Type() == nodeType {
			return NodeText(node.NamedChild(i), content)
		}
	}
	return ""
}

var jsGraphSpec = graphSpec{
	parse: ParseJSFamily,
	importNodes: map[string]func(*sitter.Node, []byte) []string{
		"import_statement": func(node *sitter.Node, content []byte) []string {
			source := ChildByField(node, "source")
			if source == nil {
				return nil
			}
			return []string{UnquoteString(NodeText(source, content))}
		},
		"call_expression": func(node *sitter.Node, content []byte) []string {
			fn := ChildByField(node, "function")
			if fn == nil || fn.Type() != "identifier" || NodeText(fn, content) != "require" {
				return nil
			}
			args := ChildByField(node, "arguments")
			if args == nil || args.NamedChildCount() == 0 {
				return nil
			}
			arg := args.NamedChild(0)
			if arg.Type() != "string" {
				return nil
			}
			return []string{UnquoteString(NodeText(arg, content))}
		},
	},
	callNodes: map[string]func(*sitter.Node, []byte) (SymbolCall, bool){
		"call_expression": func(node *sitter.Node, content []byte) (SymbolCall, bool) {
			fn := ChildByField(node, "function")
			if fn == nil {
				return SymbolCall{}, false
			}
			switch fn.Type() {
			case "identifier":
				return SymbolCall{Name: NodeText(fn, content), Kind: "function"}, true
			case "member_expression":
				prop := ChildByField(fn, "property")
				if prop == nil {
					return SymbolCall{}, false
				}
				return SymbolCall{Name: NodeText(prop, content), Kind: "method"}, true
			}
			return SymbolCall{}, false
		},
	},
}

var pyGraphSpec = graphSpec{
	parse: func(_ string, content []byte) *sitter.Tree { return ParsePython(content) },
	importNodes: map[string]func(*sitter.Node, []byte) []string{
		"import_statement": func(node *sitter.Node, content []byte) []string {
			var out []string
			for i := 0; i < int(node.NamedChildCount()); i++ {
				child := node.NamedChild(i)
				switch child.Type() {
				case "dotted_name":
					out = append(out, NodeText(child, content))
				case "aliased_import":
					if name := ChildByField(child, "name"); name != nil {
						out = append(out, NodeText(name, content))
					}
				}
			}
			return out
		},
		"import_from_statement": func(node *sitter.Node, content []byte) []string {
			module := ChildByField(node, "module_name")
			if module == nil {
				return nil
			}
			return []string{NodeText(module, content)}
		},
	},
	callNodes: map[string]func(*sitter.Node, []byte) (SymbolCall, bool){
		"call": func(node *sitter.Node, content []byte) (SymbolCall, bool) {
			fn := ChildByField(node, "function")
			if fn == nil {
				return SymbolCall{}, false
			}
			switch fn.Type() {
			case "identifier":
				return SymbolCall{Name: NodeText(fn, content), Kind: "function"}, true
			case "attribute":
				attr := ChildByField(fn, "attribute")
				if attr == nil {
					return SymbolCall{}, false
				}
				return SymbolCall{Name: NodeText(attr, content), Kind: "method"}, true
			}
			return SymbolCall{}, false
		},
	},
}

var javaGraphSpec = graphSpec{
	parse: func(_ string, content []byte) *sitter.Tree { return ParseJava(content) },
	importNodes: map[string]func(*sitter.Node, []byte) []string{
		"import_declaration": func(node *sitter.Node, content []byte) []string {
			for i := 0; i < int(node.NamedChildCount()); i++ {
				child := node.NamedChild(i)
				if child.Type() == "scoped_identifier" || child.Type() == "identifier" {
					return []string{NodeText(child, content)}
				}
			}
			return nil
		},
	},
	callNodes: map[string]func(*sitter.Node, []byte) (SymbolCall, bool){
		"method_invocation": func(node *sitter.Node, content []byte) (SymbolCall, bool) {
			name := ChildByField(node, "name")
			if name == nil {
				return SymbolCall{}, false
			}
			// An invocation with an object is a method call; a bare one is a
			// call to something in scope.
			kind := "function"
			if ChildByField(node, "object") != nil {
				kind = "method"
			}
			return SymbolCall{Name: NodeText(name, content), Kind: kind}, true
		},
		"object_creation_expression": func(node *sitter.Node, content []byte) (SymbolCall, bool) {
			typeNode := ChildByField(node, "type")
			if typeNode == nil {
				return SymbolCall{}, false
			}
			return SymbolCall{Name: NodeText(typeNode, content), Kind: "class"}, true
		},
	},
}

var kotlinGraphSpec = graphSpec{
	parse: func(_ string, content []byte) *sitter.Tree { return ParseKotlin(content) },
	importNodes: map[string]func(*sitter.Node, []byte) []string{
		"import_header": func(node *sitter.Node, content []byte) []string {
			if name := firstNamedOfType(node, "identifier", content); name != "" {
				return []string{name}
			}
			return nil
		},
	},
	callNodes: map[string]func(*sitter.Node, []byte) (SymbolCall, bool){
		"call_expression": receiverCall,
	},
}

var swiftGraphSpec = graphSpec{
	parse: func(_ string, content []byte) *sitter.Tree { return ParseSwift(content) },
	importNodes: map[string]func(*sitter.Node, []byte) []string{
		"import_declaration": func(node *sitter.Node, content []byte) []string {
			if name := firstNamedOfType(node, "identifier", content); name != "" {
				return []string{name}
			}
			return nil
		},
	},
	callNodes: map[string]func(*sitter.Node, []byte) (SymbolCall, bool){
		"call_expression": receiverCall,
	},
}
