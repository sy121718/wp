package view

import (
	"reflect"

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

// AttachPage 把分页事实注入视图结构体的 Pagination 字段（纯结构投影）。
func AttachPage(viewResult any, p pagination.Page) bool {
	if viewResult == nil {
		return false
	}
	rv := reflect.ValueOf(viewResult)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return false
	}
	elem := rv.Elem()
	if elem.Kind() != reflect.Struct {
		return false
	}
	field := elem.FieldByName(PaginationKey)
	if !field.IsValid() || !field.CanSet() {
		return false
	}
	pageVal := reflect.ValueOf(p)
	if !pageVal.Type().AssignableTo(field.Type()) {
		return false
	}
	field.Set(pageVal)
	return true
}
