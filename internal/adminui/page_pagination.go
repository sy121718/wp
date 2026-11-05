package adminui

import (
	"github.com/gin-gonic/gin"

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
	return view.AttachPage(viewResult, p)
}
