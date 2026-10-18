package adminui

// listquery.go — 页面上下文（筛选条件）的兼容出口。
//
// 实现在 **pkg/binding**（与「控制器读请求参数」同处）：它跟渲染无关，只是请求 query 的
// 白名单读取；挂在 adminui 上时，只想读筛选串的控制器得 import 整个后台壳。
//
// 本文件保留为别名，存量调用点按模块批次迁到 binding.ListQuery / binding.WithListQuery，
// 迁完即删（门禁：控制器不得 import adminui）。

import (
	"github.com/gin-gonic/gin"

	"go_wp/pkg/binding"
)

// ListQuery 本次请求 query 里白名单内的页面上下文（空值丢弃、键有序）。
func ListQuery(c *gin.Context, keys ...string) string {
	return binding.ListQuery(c, keys...)
}

// WithListQuery 把页面上下文拼到地址后面（空上下文原样返回）。
func WithListQuery(action, listQuery string) string {
	return binding.WithListQuery(action, listQuery)
}
