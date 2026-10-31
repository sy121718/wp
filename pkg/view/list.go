package view

import (
	"go_wp/pkg/pagination"
)

type PageList interface {
	Paging() pagination.Page
}

func PageListOf(v any) (pagination.Page, bool) {
	if v == nil {
		return pagination.Page{}, false
	}
	l, ok := v.(PageList)
	if !ok {
		return pagination.Page{}, false
	}
	return l.Paging(), true
}

const PaginationKey = "Pagination"
