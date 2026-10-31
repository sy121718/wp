package navigationcontract

type TranslationSourceEntry struct {
	Hash string

	Source string

	Context string

	Kinds []string

	NodeID string

	Path string
}

type SourceIndex interface {
	Locate(hash string) (TranslationSourceEntry, bool)

	Entries() []TranslationSourceEntry

	Kinds() []string

	KindErrors() []KindReadError

	Require() error
}

type SourceIndexNodes interface {
	Nodes(hash string) []NavigationNodeRef
}

type NavigationNodeRef struct {
	ID string

	Kind string

	Path string
}

type KindReadError struct {
	Kind string

	Err error
}
