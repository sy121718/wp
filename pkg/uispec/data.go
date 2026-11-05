package uispec

type Column struct {
	Label string

	Align string
}

type TableData struct {
	Columns []Column

	Rows [][]string

	Empty string
}

type Item struct {
	Label string

	Value string

	Hint string
}

type ListData struct {
	Items []Item
	Empty string
}

type Stat struct {
	Label string
	Value string

	Delta string

	Trend string
}

type StatData struct {
	Items []Stat

	Empty string
}

type Group struct {
	Title string

	Body string

	Items []Item
}

type AccordionData struct {
	Groups []Group
	Empty  string
}
