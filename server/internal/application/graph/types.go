package graph

type EdgeKind string

const (
	EdgeCall   EdgeKind = "call"
	EdgeImport EdgeKind = "import"
)

type SymbolRef struct {
	FilePath   string
	SymbolName string
	Kind       string
}

func (s SymbolRef) Key() string {
	return s.FilePath + ":" + s.SymbolName
}

type Edge struct {
	From SymbolRef
	To   SymbolRef
	Kind EdgeKind
}

type DependencyGraph struct {
	edges    []Edge
	outgoing map[string][]Edge
	byFile   map[string][]Edge
}

func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		outgoing: make(map[string][]Edge),
		byFile:   make(map[string][]Edge),
	}
}

func (g *DependencyGraph) AddEdge(e Edge) {
	g.edges = append(g.edges, e)
	fromKey := e.From.Key()
	g.outgoing[fromKey] = append(g.outgoing[fromKey], e)
	g.byFile[e.From.FilePath] = append(g.byFile[e.From.FilePath], e)
}

func (g *DependencyGraph) Edges() []Edge {
	return append([]Edge(nil), g.edges...)
}

func (g *DependencyGraph) Outgoing(from SymbolRef) []Edge {
	return append([]Edge(nil), g.outgoing[from.Key()]...)
}

func (g *DependencyGraph) OutgoingCalls(from SymbolRef) []SymbolRef {
	var refs []SymbolRef
	for _, e := range g.outgoing[from.Key()] {
		if e.Kind == EdgeCall {
			refs = append(refs, e.To)
		}
	}
	return refs
}

func (g *DependencyGraph) Imports(filePath string) []string {
	var paths []string
	for _, e := range g.byFile[filePath] {
		if e.Kind == EdgeImport {
			paths = append(paths, e.To.SymbolName)
		}
	}
	return paths
}

type SymbolLookup interface {
	Lookup(filePath, symbolName string) (SymbolRef, bool)
}
