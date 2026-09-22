package pipeline

type Spec struct {
	Platform   string
	Identifier string
	AppName    string
	StoreAppID string

	Scheme string
	Module string

	SubProjectPath string
}

type Artifact struct {
	Path string
	Body string
	Mode uint32
}
