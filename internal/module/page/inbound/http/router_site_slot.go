package pagehttp

// router_site_slot.go — 系统页面槽位页路由（结算页绑定/解绑）。
//
// 页面挂在装配层传入的 /admin 组上：该组已有 Session + CSRF + 权限上下文中间件
// （见 internal/routers/assembly.go 的 adminPages）。写动作额外按**对应 API 的路径**
// 走 Casbin 权限点 —— 与 /api/page/site-slot/* 的权限点完全同源，一个字符都不改。
// pages 为 nil 时跳过注册：模块装配不因缺少页面组而失败，与 rg 的既有语义同构。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
)

// setupSiteSlotPageRoutes 注册系统页面槽位页（pages = /admin 页面组）。
//
// 系统页面槽位（BIZ-1）：把「结算页是哪一页」这类事实固定下来。
// 侧栏入口是 sys_menus 里「内容」分组下的「系统页面」（迁移 140 落行，224 收口归位）。
func setupSiteSlotPageRoutes(pages *gin.RouterGroup, svc pagecontract.PageService,
	projects projectcontract.ProjectService) {
	if pages == nil || svc == nil {
		return
	}
	h := NewSiteSlotPageHandle(svc, projects)
	pages.GET("/site-slots", h.SiteSlotsPage)
	pages.POST("/site-slots/bind", builtin.CasbinMiddlewareForPath("/api/page/site-slot/bind"), h.SiteSlotBind)
	pages.POST("/site-slots/unbind", builtin.CasbinMiddlewareForPath("/api/page/site-slot/unbind"), h.SiteSlotUnbind)
}
