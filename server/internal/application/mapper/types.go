package mapper

type Symbol struct {
	Kind      string
	Name      string
	Signature string
	Doc       string
	Receiver  string
}

type FileSkeleton struct {
	Path    string
	Package string
	Doc     string
	Imports []string
	Symbols []Symbol
}
