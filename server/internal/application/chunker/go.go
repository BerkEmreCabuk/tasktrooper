package chunker

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

const goMaxFuncLines = 150

type GoChunker struct{}

func (GoChunker) Chunk(filePath string, content []byte) ([]Chunk, error) {
	src := string(content)
	if strings.TrimSpace(src) == "" {
		return nil, nil
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filePath, src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse go file: %w", err)
	}

	var chunks []Chunk
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			chunks = append(chunks, chunkFuncDecl(fset, filePath, src, d)...)
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				startLine := fset.Position(ts.Pos()).Line
				endLine := fset.Position(ts.End()).Line
				chunks = append(chunks, Chunk{
					FilePath:   filePath,
					Language:   "go",
					Kind:       "type",
					SymbolName: ts.Name.Name,
					StartLine:  startLine,
					EndLine:    endLine,
					Content:    extractLines(src, startLine, endLine),
					Signature:  typeSignature(src, ts, fset),
				})
			}
		}
	}
	return chunks, nil
}

func chunkFuncDecl(fset *token.FileSet, filePath, src string, decl *ast.FuncDecl) []Chunk {
	startLine := fset.Position(decl.Pos()).Line
	endLine := fset.Position(decl.End()).Line
	kind := "function"
	symbolName := decl.Name.Name
	if decl.Recv != nil {
		kind = "method"
		recv := exprString(decl.Recv.List[0].Type)
		symbolName = recv + "." + decl.Name.Name
	}

	lineCount := endLine - startLine + 1
	if lineCount <= goMaxFuncLines {
		return []Chunk{{
			FilePath:   filePath,
			Language:   "go",
			Kind:       kind,
			SymbolName: symbolName,
			StartLine:  startLine,
			EndLine:    endLine,
			Content:    extractLines(src, startLine, endLine),
			Signature:  funcSignature(src, decl, fset),
		}}
	}

	if decl.Body == nil {
		return []Chunk{{
			FilePath:   filePath,
			Language:   "go",
			Kind:       kind,
			SymbolName: symbolName,
			StartLine:  startLine,
			EndLine:    endLine,
			Content:    extractLines(src, startLine, endLine),
			Signature:  funcSignature(src, decl, fset),
		}}
	}

	var blocks []*ast.BlockStmt
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		block, ok := n.(*ast.BlockStmt)
		if !ok || block == decl.Body {
			return true
		}
		bStart := fset.Position(block.Pos()).Line
		bEnd := fset.Position(block.End()).Line
		if bEnd-bStart+1 >= 5 {
			blocks = append(blocks, block)
		}
		return true
	})

	if len(blocks) == 0 {
		return lineSplitFunc(filePath, src, kind, symbolName, funcSignature(src, decl, fset), startLine, endLine)
	}

	sig := funcSignature(src, decl, fset)
	var chunks []Chunk
	for i, block := range blocks {
		bStart := fset.Position(block.Pos()).Line
		bEnd := fset.Position(block.End()).Line
		chunks = append(chunks, Chunk{
			FilePath:   filePath,
			Language:   "go",
			Kind:       "block",
			SymbolName: fmt.Sprintf("%s.block%d", symbolName, i+1),
			StartLine:  bStart,
			EndLine:    bEnd,
			Content:    extractLines(src, bStart, bEnd),
			Signature:  sig,
		})
	}
	return chunks
}

func lineSplitFunc(filePath, src, kind, symbolName, signature string, startLine, endLine int) []Chunk {
	lines := strings.Split(src, "\n")
	funcLines := lines[startLine-1 : endLine]
	var chunks []Chunk
	for start := 0; start < len(funcLines); {
		end := start + goMaxFuncLines
		if end > len(funcLines) {
			end = len(funcLines)
		}
		chunks = append(chunks, Chunk{
			FilePath:   filePath,
			Language:   "go",
			Kind:       "block",
			SymbolName: fmt.Sprintf("%s.block%d", symbolName, len(chunks)+1),
			StartLine:  startLine + start,
			EndLine:    startLine + end - 1,
			Content:    strings.Join(funcLines[start:end], "\n"),
			Signature:  signature,
		})
		if end >= len(funcLines) {
			break
		}
		next := end - 20
		if next <= start {
			next = end
		}
		start = next
	}
	return chunks
}

func funcSignature(src string, decl *ast.FuncDecl, fset *token.FileSet) string {
	line := fset.Position(decl.Pos()).Line
	lines := strings.Split(src, "\n")
	if line >= 1 && line <= len(lines) {
		sig := strings.TrimSpace(lines[line-1])
		if strings.Contains(sig, "{") {
			sig = strings.Split(sig, "{")[0]
		}
		return strings.TrimSpace(sig)
	}
	return decl.Name.Name + "(...)"
}

func typeSignature(src string, ts *ast.TypeSpec, fset *token.FileSet) string {
	line := fset.Position(ts.Pos()).Line
	lines := strings.Split(src, "\n")
	if line >= 1 && line <= len(lines) {
		return strings.TrimSpace(lines[line-1])
	}
	return "type " + ts.Name.Name
}

func exprString(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + exprString(t.X)
	case *ast.SelectorExpr:
		return exprString(t.X) + "." + t.Sel.Name
	default:
		return "unknown"
	}
}
