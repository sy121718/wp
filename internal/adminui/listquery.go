package adminui

import (
	"github.com/gin-gonic/gin"

	"go_wp/pkg/binding"
)

func ListQuery(c *gin.Context, keys ...string) string {
	return binding.ListQuery(c, keys...)
}

func WithListQuery(action, listQuery string) string {
	return binding.WithListQuery(action, listQuery)
}
