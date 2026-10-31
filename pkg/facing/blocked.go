package facing

type BlockedItem struct {
	ID        string
	Name      string
	ReasonKey string
	Fallback  string
}

type BlockedError interface {
	error
	BlockedItems() []BlockedItem
}
