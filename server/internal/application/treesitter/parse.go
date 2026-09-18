package treesitter

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/csharp"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/kotlin"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/swift"
	tsx "github.com/smacker/go-tree-sitter/typescript/tsx"
	ts "github.com/smacker/go-tree-sitter/typescript/typescript"
)

func ParseTypeScript(content []byte) *sitter.Tree {
	parser := sitter.NewParser()
	parser.SetLanguage(ts.GetLanguage())
	return parser.Parse(nil, content)
}

// ParseTSX handles the JSX-bearing dialects. The plain TypeScript grammar
// cannot parse a JSX element, so a .tsx component parsed with ParseTypeScript
// comes back as one big ERROR node — which is how React components used to
// yield no symbols at all.
func ParseTSX(content []byte) *sitter.Tree {
	parser := sitter.NewParser()
	parser.SetLanguage(tsx.GetLanguage())
	return parser.Parse(nil, content)
}

func ParseJava(content []byte) *sitter.Tree {
	parser := sitter.NewParser()
	parser.SetLanguage(java.GetLanguage())
	return parser.Parse(nil, content)
}

func ParseCSharp(content []byte) *sitter.Tree {
	parser := sitter.NewParser()
	parser.SetLanguage(csharp.GetLanguage())
	return parser.Parse(nil, content)
}

func ParseKotlin(content []byte) *sitter.Tree {
	parser := sitter.NewParser()
	parser.SetLanguage(kotlin.GetLanguage())
	return parser.Parse(nil, content)
}

func ParseSwift(content []byte) *sitter.Tree {
	parser := sitter.NewParser()
	parser.SetLanguage(swift.GetLanguage())
	return parser.Parse(nil, content)
}

func ParseJavaScript(content []byte) *sitter.Tree {
	parser := sitter.NewParser()
	parser.SetLanguage(javascript.GetLanguage())
	return parser.Parse(nil, content)
}

func ParsePython(content []byte) *sitter.Tree {
	parser := sitter.NewParser()
	parser.SetLanguage(python.GetLanguage())
	return parser.Parse(nil, content)
}

// ParseJSFamily picks the grammar matching path's dialect: TSX for JSX-bearing
// files, plain TypeScript for .ts/.mts/.cts, JavaScript for everything else.
func ParseJSFamily(path string, content []byte) *sitter.Tree {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".tsx"), strings.HasSuffix(lower, ".jsx"):
		return ParseTSX(content)
	case strings.HasSuffix(lower, ".ts"), strings.HasSuffix(lower, ".mts"), strings.HasSuffix(lower, ".cts"):
		return ParseTypeScript(content)
	default:
		return ParseJavaScript(content)
	}
}

func NodeText(node *sitter.Node, content []byte) string {
	if node == nil {
		return ""
	}
	return string(content[node.StartByte():node.EndByte()])
}

func ChildByField(node *sitter.Node, field string) *sitter.Node {
	if node == nil {
		return nil
	}
	return node.ChildByFieldName(field)
}

func HeaderText(node *sitter.Node, content []byte, bodyField string) string {
	if node == nil {
		return ""
	}
	end := node.EndByte()
	if body := ChildByField(node, bodyField); body != nil {
		end = body.StartByte()
	}
	text := strings.TrimSpace(string(content[node.StartByte():end]))
	text = strings.ReplaceAll(text, "\n", " ")
	return strings.Join(strings.Fields(text), " ")
}

func UnquoteString(literal string) string {
	literal = strings.TrimSpace(literal)
	if strings.HasPrefix(literal, `"""`) && strings.HasSuffix(literal, `"""`) && len(literal) >= 6 {
		return strings.TrimSpace(literal[3 : len(literal)-3])
	}
	if strings.HasPrefix(literal, "'''") && strings.HasSuffix(literal, "'''") && len(literal) >= 6 {
		return strings.TrimSpace(literal[3 : len(literal)-3])
	}
	if len(literal) < 2 {
		return literal
	}
	switch literal[0] {
	case '"', '\'':
		if literal[len(literal)-1] == literal[0] {
			return literal[1 : len(literal)-1]
		}
	case '`':
		if literal[len(literal)-1] == '`' {
			return literal[1 : len(literal)-1]
		}
	}
	return literal
}

func IsTypeScriptPath(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".ts") || strings.HasSuffix(lower, ".tsx")
}

func WalkNamed(node *sitter.Node, fn func(*sitter.Node)) {
	if node == nil {
		return
	}
	fn(node)
	for i := 0; i < int(node.NamedChildCount()); i++ {
		WalkNamed(node.NamedChild(i), fn)
	}
}
