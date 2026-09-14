package graph

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

func BuildGoCallGraph(filePath string, content []byte) ([]Edge, error) {
	src := string(content)
	if strings.TrimSpace(src) == "" {
		return nil, nil
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parse go file: %w", err)
	}

	var edges []Edge
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		from := goFuncRef(filePath, fn)
		if fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			callee := calleeName(call.Fun)
			if callee == "" {
				return true
			}
			edges = append(edges, Edge{
				From: from,
				To: SymbolRef{
					FilePath:   filePath,
					SymbolName: callee,
					Kind:       "function",
				},
				Kind: EdgeCall,
			})
			return true
		})
	}
	return edges, nil
}

func goFuncRef(filePath string, fn *ast.FuncDecl) SymbolRef {
	name := fn.Name.Name
	kind := "function"
	if fn.Recv != nil {
		kind = "method"
		recv := goExprName(fn.Recv.List[0].Type)
		name = recv + "." + fn.Name.Name
	}
	return SymbolRef{
		FilePath:   filePath,
		SymbolName: name,
		Kind:       kind,
	}
}

func calleeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	default:
		return ""
	}
}

func goExprName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return goExprName(t.X)
	case *ast.SelectorExpr:
		return goExprName(t.X) + "." + t.Sel.Name
	default:
		return "unknown"
	}
}
