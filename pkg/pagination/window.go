package pagination

const WindowSize = 7

type Item struct {
	Gap  bool
	Page int
	URL  string
}

func Window(page, pages int) []Item {
	if pages <= 1 {
		return nil
	}
	if page < 1 {
		page = 1
	}
	if page > pages {
		page = pages
	}
	if pages <= WindowSize {
		out := make([]Item, 0, pages)
		for p := 1; p <= pages; p++ {
			out = append(out, Item{Page: p})
		}
		return out
	}

	half := (WindowSize - 3) / 2
	lo := page - half
	hi := page + half
	if lo < 2 {
		lo, hi = 2, lo+WindowSize-3
	}
	if hi > pages-1 {
		hi, lo = pages-1, pages-1-(WindowSize-3)
	}

	out := make([]Item, 0, WindowSize)
	out = append(out, Item{Page: 1})
	if lo > 2 {
		out = append(out, Item{Gap: true})
	}
	for p := lo; p <= hi; p++ {
		if p < 2 || p > pages-1 {
			continue
		}
		out = append(out, Item{Page: p})
	}
	if hi < pages-1 {
		out = append(out, Item{Gap: true})
	}
	return append(out, Item{Page: pages})
}
