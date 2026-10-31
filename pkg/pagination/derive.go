package pagination

type Derived struct {
	Total int64
	Page  int
	Limit int
	Pages int
	Start int
	End   int

	PageParam string
	KeepLimit bool
	Base      string
}

func Derive(p Page) Derived {
	limit := p.Limit
	if limit < 1 {
		limit = DefaultPageSize
	}
	page := p.Page
	if page < 1 {
		page = 1
	}
	pages := 0
	if p.Total > 0 {
		pages = int((p.Total + int64(limit) - 1) / int64(limit))
	}
	if pages > 0 && page > pages {
		page = pages
	}
	d := Derived{
		Total: p.Total, Page: page, Limit: limit, Pages: pages,
		PageParam: p.PageParam, KeepLimit: !p.DropLimit, Base: p.Base,
	}
	if d.PageParam == "" {
		d.PageParam = "page"
	}
	if p.Total > 0 {
		d.Start = (page-1)*limit + 1
		d.End = page * limit
		if int64(d.End) > p.Total {
			d.End = int(p.Total)
		}
	}
	return d
}

func (d Derived) Items() []Item {
	win := Window(d.Page, d.Pages)
	if len(win) == 0 {
		return nil
	}
	for i := range win {
		if win[i].Gap || win[i].Page == d.Page {
			continue
		}
		win[i].URL = d.PageURL(win[i].Page)
	}
	return win
}

func (d Derived) PageURL(n int) string {
	if d.KeepLimit {
		return QueryURL(d.Base, d.PageParam, n, "limit", d.Limit)
	}
	return QueryURL(d.Base, d.PageParam, n)
}

func (d Derived) PrevURL() string {
	if !d.HasPrev() {
		return ""
	}
	return d.PageURL(d.Page - 1)
}

func (d Derived) NextURL() string {
	if !d.HasNext() {
		return ""
	}
	return d.PageURL(d.Page + 1)
}

func (d Derived) HasPagination() bool { return d.Pages > 1 }

func (d Derived) HasPrev() bool { return d.Page > 1 }
func (d Derived) HasNext() bool { return d.Page < d.Pages }

func (d Derived) PrevPage() int { return d.Page - 1 }
func (d Derived) NextPage() int { return d.Page + 1 }

func (d Derived) WithBase(base string) Derived {
	d.Base = base
	return d
}
