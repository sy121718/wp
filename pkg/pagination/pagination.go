package pagination

type Page struct {
	Total int64
	Page  int
	Limit int

	PageParam string
	DropLimit bool
	Base      string
}
