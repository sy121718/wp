package dashboardhttp

import (
	"go_wp/internal/middleware/builtin"

	"github.com/gin-gonic/gin"
)

// router_site_slot.go - 系统页面槽位页路由（结算页绑定/解绑）。

func setupSiteSlotRoutes(adminPages *gin.RouterGroup, d *routeDeps, handle *Handle) {
	// 系统页面槽位（BIZ-1）：把「结算页是哪一页」这类事实固定下来。
	// 页面 GET 走 /admin 组认证（Session+CSRF，无 Casbin）；写动作复用槽位 API 权限点（迁移 139）。
	// 侧栏入口见 nav_menu.go 的 system 组（迁移 140 只 seed 了 sys_menus，后台侧栏读的是 navConfig）。
	siteSlotPages := NewSiteSlotPageHandle(d.pages, d.projects)
	adminPages.GET("/site-slots", siteSlotPages.SiteSlotsPage)
	adminPages.POST("/site-slots/bind", builtin.CasbinMiddlewareForPath("/api/page/site-slot/bind"), siteSlotPages.SiteSlotBind)
	adminPages.POST("/site-slots/unbind", builtin.CasbinMiddlewareForPath("/api/page/site-slot/unbind"), siteSlotPages.SiteSlotUnbind)
}
