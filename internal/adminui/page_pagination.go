package adminui

import (
	"github.com/gin-gonic/gin"
	"reflect"

	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/view"
)

func AttachPagination(data gin.H, path string, filters map[string]string, p pagination.Page) gin.H {
	if data == nil {
		return data
	}
	if path != "" {
		p.Base = filterBaseURL(path, filters)
	}
	data[view.PaginationKey] = p
	return data
}

func filterBaseURL(path string, filters map[string]string) string {
	return reqparams.FilterBaseURL(path, filters)
}

func AttachPaginationView(viewResult any, path string, filters map[string]string, p pagination.Page) bool {
	if viewResult == nil {
		return false
	}
	if path != "" {
		p.Base = filterBaseURL(path, filters)
	}
	rv := reflect.ValueOf(viewResult)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return false
	}
	elem := rv.Elem()
	if elem.Kind() != reflect.Struct {
		return false
	}
	field := elem.FieldByName(view.PaginationKey)
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
