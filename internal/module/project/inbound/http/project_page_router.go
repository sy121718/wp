package projecthttp

// project_page_router.go — 主题管理 / 站点设置 / 编辑器面板的注册落点。
//
// 只做注册：`/admin` 与工作台组的中间件链由装配层统一挂好，handler 在
// project_page.go / project_handle.go。路由注册只出现在 *_router.go，门禁
// scripts/check-route-registration-placement.sh 守这条。
//
// 页面写动作的 Casbin 权限点路径与迁移前逐字一致；页面 GET 的 Casbin 待补
//（见 AGENTS.md §4.3）。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	blockcontract "go_wp/internal/module/block/contract"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
)

// SetupProjectPages 注册主题 / 站点设置页面（adminPages）与编辑器面板（workbenchPages）。
// 独立入口的原因与 content 同构：页面依赖的 page / block 契约晚于 project 装配。
// 项目设置相关三屏的模板名（页面出口由页面槽带进上下文）。
const (
	ThemeManagePageTemplate   = "admin/project/theme"
	ThemeSettingsPageTemplate = "admin/project/theme_settings"
	SiteSettingsPageTemplate  = "admin/project/settings"

	// 片段出口（htmx 局部替换）：键集只有几个槽位，走 BareSlot。
	LocaleRowsFragmentTemplate    = "admin/partials/locale_rows"
	GlobalPanelFragmentTemplate   = "fragments/global_panel"
	SettingsPanelFragmentTemplate = "fragments/settings_panel"
	SeoScoreFragmentTemplate      = "fragments/seo_score"
)

func SetupProjectPages(adminPages, workbenchPages *gin.RouterGroup,
	svc projectcontract.ProjectService, pages pagecontract.PageService, blocks blockcontract.BlockService,
	dict sysconfigcontract.DictReader) {
	if adminPages != nil {
		themes := &themeAdminHandle{projects: svc, pages: pages, blocks: blocks}
		adminPages.GET("/themes",
			adminui.PageAuthz("/api/theme/list"),
			adminui.PageSlot(ThemeManagePageTemplate),
			themes.ThemeManage)
		adminPages.POST("/themes/create", builtin.CasbinMiddlewareForPath("/api/theme/create"), themes.CreateTheme)
		adminPages.POST("/themes/activate", builtin.CasbinMiddlewareForPath("/api/theme/activate"), themes.ActivateTheme)
		adminPages.POST("/themes/delete", builtin.CasbinMiddlewareForPath("/api/theme/delete"), themes.DeleteTheme)
		adminPages.GET("/themes/settings",
			adminui.PageAuthz("/api/theme/list"),
			adminui.PageSlot(ThemeSettingsPageTemplate),
			themes.ThemeSettings)
		adminPages.POST("/themes/settings/save", builtin.CasbinMiddlewareForPath("/api/theme/update"), themes.SaveThemeSettings)
		adminPages.GET("/theme", themes.ThemeRedirect)

		settings := &siteSettingsAdminHandle{projects: svc, pages: pages, dict: dict}
		adminPages.GET("/settings",
			adminui.PageAuthz("/api/project/detail"),
			adminui.PageSlot(SiteSettingsPageTemplate),
			settings.SiteSettings)
		adminPages.POST("/settings/save", builtin.CasbinMiddlewareForPath("/api/project/update"), settings.SaveSiteSettings)
		adminPages.POST("/settings/locales/rows", builtin.CasbinMiddlewareForPath("/api/project/update"),
			adminui.BareSlot(LocaleRowsFragmentTemplate), settings.LocaleRowsFragment)
		adminPages.POST("/settings/locales/save", builtin.CasbinMiddlewareForPath("/api/project/update"), settings.SaveSiteLocales)
	}
	if workbenchPages != nil {
		workbenchPages.POST("/workbench/settings",
			adminui.BareSlot(SettingsPanelFragmentTemplate), workbenchSettingsPanel)
		workbenchPages.POST("/workbench/seo-score-panel",
			adminui.BareSlot(SeoScoreFragmentTemplate), workbenchSeoScorePanel)
		workbenchPages.POST("/workbench/global",
			adminui.BareSlot(GlobalPanelFragmentTemplate), workbenchGlobalPanel)
	}
}
