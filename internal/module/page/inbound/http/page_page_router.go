package pagehttp

// page_page_router.go — page 模块后台页面（/admin/pages*、/admin/page-langs*、/admin/site-slots、
// /workbench/history*）的注册落点。
//
// 只做注册：`/admin` 与编辑器根级页面组的中间件链（Session + CSRF + 权限上下文）由装配层
// 统一挂好，handler 在 pages_handle.go / page_*_handle.go。路由注册只出现在 *_router.go，
// 门禁 scripts/check-route-registration-placement.sh 守这条。
//
// 写动作额外按**对应 API 的路径**走 Casbin 权限点（页面路径与权限点路径不一致，
// 直接按页面路径 enforce 会因权限点表无此路径而拒绝所有用户）。页面 GET 的 Casbin 待补
// （见 docs/02-Z-admin-menu-code-and-page-authz.md §4.3）。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	blockcontract "go_wp/internal/module/block/contract"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/shell"
)

// SetupPageAdminPages 注册页面域后台页面与编辑器修订历史面板。
// 两个页面组任一为 nil 时只跳过对应部分 —— 与 rg 的既有语义同构。
//
// 注意：系统页面槽位页（setupSiteSlotPageRoutes，本文件下方）仍由 page_router.go 在
// 原位置调用 —— 这里不能再调一次，gin 对重复路由直接 panic。
func SetupPageAdminPages(pages, workbenchPages *gin.RouterGroup, svc pagecontract.PageService,
	projectService projectcontract.ProjectService, blocks blockcontract.BlockService, handle *Handle) {
	adminHandle := NewPagesAdminHandle(svc, projectService, blocks, nil)
	if pages != nil {
		pages.GET("/pages", shell.PageAuthz("/api/page/list"), adminHandle.PagesList)
		pages.POST("/pages/create", builtin.CasbinMiddlewareForPath("/api/page/create"), adminHandle.CreatePage)
		// 单条删除与批量删除复用「删除页面」权限点（/api/page/delete，迁移 151）：
		// 两者走同一个 svc.Delete —— 权限点、拒绝规则、访问面下线动作都不会分叉。
		// 批量删除不能自成一个权限点：它只是单条删除的加速器，不是另一件事。
		pages.POST("/pages/delete", builtin.CasbinMiddlewareForPath("/api/page/delete"), adminHandle.DeletePage)
		pages.POST("/pages/bulk-delete", builtin.CasbinMiddlewareForPath("/api/page/delete"), adminHandle.PagesBulkDelete)
		// 重定向的批量删除挂**后台页面组**、而不是 /api 组：
		// authorizedAPI 组统一按实际请求路径 enforce（scripts/check-permission-gaps.sh 专盯这条），
		// 新路径在 sys_permission 里没有条目 → 含超管在内一律 403；而补一条权限点必须写迁移，
		// 批量删除只是单条删除的加速器，不值得为它单开权限点。
		// 于是与商品 / 导航 / 文案三个域的批量端点同构：挂 /admin 组（Session + CSRF 已具备）
		// + 显式复用单条删除的权限点路径 /api/page/redirect/delete。
		pages.POST("/page-redirects/bulk-delete", builtin.CasbinMiddlewareForPath("/api/page/redirect/delete"), handle.RedirectBulkDelete)
		// 翻译工作台（多语言 P5c，docs/06-D §7.8）：入口在页面列表行内「多语言」按钮。
		// 保存写 sys_translation（engine=manual）并触发全站标记待重建，鉴权复用「保存草稿」权限点。
		pages.GET("/pages/translations", shell.PageAuthz("/api/page/list"), adminHandle.PageTranslations)
		// 定时上下线的面板与表单（PIPE-7）：面板是 HTMX 片段（列表页行内「定时」按钮的落点），
		// 两个 POST 是原生表单（form-urlencoded + 隐藏 csrf_token 域），鉴权复用 API 的权限点路径
		// —— 页面路径与权限点路径不一致，直接按页面路径 enforce 会因权限点表无此路径而拒绝所有用户
		// （与 /pages/page-redirects/bulk-delete 同一手法）。
		pages.GET("/page-schedules/panel", builtin.CasbinMiddlewareForPath("/api/page/schedule/list"), adminHandle.SchedulePanel)
		pages.POST("/page-schedules/set", builtin.CasbinMiddlewareForPath("/api/page/schedule/set"), adminHandle.ScheduleSet)
		pages.POST("/page-schedules/cancel", builtin.CasbinMiddlewareForPath("/api/page/schedule/cancel"), adminHandle.ScheduleCancel)
		pages.POST("/pages/translations/save", builtin.CasbinMiddlewareForPath("/api/page/draft/save"), adminHandle.SavePageTranslations)
		// 页面级语言排除（迁移 491）：面板展示本页各语言的产出范围（默认语言 / 已发布 /
		// 已排除），可排除与恢复。**排除会真的下线该语言产物**（pageservice.ExcludePageLang
		// 在同一事务里清发布/暂存/路由/计划），因此鉴权复用「发布页面」权限点；
		// 恢复只改产出范围（不自动重新发布），鉴权同一条 —— 两者都是「这一页发不发这种语言」
		// 的同一件事，不该拆成两个权限点。
		pages.GET("/page-langs/panel", builtin.CasbinMiddlewareForPath("/api/page/detail"), adminHandle.PageLangsPanel)
		pages.POST("/page-langs/exclude", builtin.CasbinMiddlewareForPath("/api/page/publish"), adminHandle.PageLangExclude)
		pages.POST("/page-langs/restore", builtin.CasbinMiddlewareForPath("/api/page/publish"), adminHandle.PageLangRestore)
		// 显式重新发布（V4）：恢复排除只解除限制、不自动上线 —— 译好后要真的回到线上，
		// 需要一个一次点击的入口（同步 Build + Publish，结果当场可见）。权限点同上。
		pages.POST("/page-langs/republish", builtin.CasbinMiddlewareForPath("/api/page/publish"), adminHandle.PageLangRepublish)
		// 缺译报告（U2）：按 页面 × 语言 列出内容缺译，操作列直接调 ExcludePageLang（不另写下线逻辑）。
		// 挂后台页面组（与 /page-schedules/* 同形），写操作显式复用「发布页面」权限点。
		pages.GET("/page-translation-misses", builtin.CasbinMiddlewareForPath("/api/page/list"), adminHandle.TranslationMissesPage)
		pages.POST("/page-translation-misses/cancel", builtin.CasbinMiddlewareForPath("/api/page/publish"), adminHandle.TranslationMissCancel)
		pages.POST("/projects/create", builtin.CasbinMiddlewareForPath("/api/project/create"), adminHandle.CreateProject)
	}
	// 修订历史列表与恢复（HTMX 化，docs/09 §3）：挂编辑器根级页面组。
	// 恢复修订会覆盖页面草稿（写操作），权限点复用「保存草稿」——
	// 否则任何仅登录后台的低权限用户都能覆盖任意页面草稿。
	if workbenchPages != nil {
		workbenchPages.POST("/workbench/history", adminHandle.HistoryPanel)
		workbenchPages.POST("/workbench/history/restore", builtin.CasbinMiddlewareForPath("/api/page/draft/save"), adminHandle.HistoryRestore)
	}
}

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
	pages.GET("/site-slots", shell.PageAuthz("/api/page/list"), h.SiteSlotsPage)
	pages.POST("/site-slots/bind", builtin.CasbinMiddlewareForPath("/api/page/site-slot/bind"), h.SiteSlotBind)
	pages.POST("/site-slots/unbind", builtin.CasbinMiddlewareForPath("/api/page/site-slot/unbind"), h.SiteSlotUnbind)
}
