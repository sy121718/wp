// Package dashboardhttp 承载需要后端逻辑的后台页面入口（当前为仪表盘）。
//
// 纯静态模板直接放 internal/templates；只有需要后端数据/逻辑的页面才落到本模块。
// 可视化编辑器前端组件已废弃移除，待按设计文档（docs/）重新实现后再挂载页面入口。
package dashboardhttp

import (
	"net/http"

	dashboardenums "go_wp/internal/module/dashboard/enums"

	"github.com/gin-gonic/gin"
)

// Handle 页面处理器，聚合 dashboard 相关 handler。
type Handle struct{}

// NewHandle 创建页面处理器。
func NewHandle() *Handle {
	return &Handle{}
}

// Dashboard 仪表盘页面。
func (h *Handle) Dashboard(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/dashboard", gin.H{
		"title": dashboardenums.MsgDashboardTitle,
		"menu":  "dashboard",
	})
}