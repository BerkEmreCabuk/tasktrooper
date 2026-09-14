package mapper

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

func extractGoSkeleton(path string, content []byte) (FileSkeleton, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, content, parser.ParseComments)
	if err != nil {
		return FileSkeleton{}, err
	}

	sk := FileSkeleton{
		Path:    path,
		Package: file.Name.Name,
	}

	if file.Doc != nil {
		sk.Doc = strings.TrimSpace(file.Doc.Text())
	}

	for _, imp := range file.Imports {
		name := strings.Trim(imp.Path.Value, `"`)
		sk.Imports = append(sk.Imports, name)
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			kind := "func"
			receiver := ""
			if d.Recv != nil && len(d.Recv.List) > 0 {
				kind = "method"
				receiver = typeExprString(d.Recv.List[0].Type)
			}
			doc := ""
			if d.Doc != nil {
				doc = strings.TrimSpace(d.Doc.Text())
			}
			sk.Symbols = append(sk.Symbols, Symbol{
				Kind:     kind,
				Name:     d.Name.Name,
				Doc:      doc,
				Receiver: receiver,
			})
		case *ast.GenDecl:
			doc := ""
			if d.Doc != nil {
				doc = strings.TrimSpace(d.Doc.Text())
			}
			switch d.Tok {
			case token.CONST:
				for _, spec := range d.Specs {
					valueDoc := doc
					if ts, ok := spec.(*ast.ValueSpec); ok {
						if ts.Doc != nil {
							valueDoc = strings.TrimSpace(ts.Doc.Text())
						}
						for _, name := range ts.Names {
							sk.Symbols = append(sk.Symbols, Symbol{
								Kind: "const",
								Name: name.Name,
								Doc:  valueDoc,
							})
						}
					}
				}
			case token.TYPE:
				for _, spec := range d.Specs {
					valueDoc := doc
					if ts, ok := spec.(*ast.TypeSpec); ok {
						if ts.Doc != nil {
							valueDoc = strings.TrimSpace(ts.Doc.Text())
						}
						kind := "type"
						switch ts.Type.(type) {
						case *ast.InterfaceType:
							kind = "interface"
						}
						sk.Symbols = append(sk.Symbols, Symbol{
							Kind: kind,
							Name: ts.Name.Name,
							Doc:  valueDoc,
						})
					}
				}
			}
		}
	}

	return sk, nil
}

func typeExprString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + typeExprString(t.X)
	case *ast.SelectorExpr:
		return typeExprString(t.X) + "." + t.Sel.Name
	default:
		return ""
	}
}
