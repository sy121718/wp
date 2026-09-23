package projecthttp

import (
	"go_wp/internal/middleware/builtin"
	blockcontract "go_wp/internal/module/block/contract"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/permission"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupProjectRoutes 自装配 project 模块并注册路由，返回契约供 page/build 等模块依赖。
//
// adminPages / workbenchPages 是装配层创建的页面组（Session + CSRF + 权限上下文）：
// 主题管理 / 站点设置 / 多语言行挂 adminPages，编辑器设置面板（SettingsPanel /
// GlobalPanel）挂 workbenchPages。pages 与 blocks 为页面换皮编排与主题设置只读取数
// 所需的 page/block 契约，由装配层注入。
func SetupProjectRoutes(rg *permission.RouteGroup, db *gorm.DB) projectcontract.ProjectService {
	model := projectmodel.NewProjectModel(db)
	svc := projectservice.NewService(model)
	handle := NewHandle(svc)
	restoreLangURLMode(svc)

	SetupThemeRoutes(rg, db)
	g := rg.Group("/project", builtin.SessionAuthMiddleware())
	g.POST("/create", permission.ProjectCreate, handle.Create)
	g.GET("/list", permission.ProjectList, handle.List)
	g.GET("/detail", permission.ProjectDetail, handle.Detail)
	g.POST("/update", permission.ProjectUpdate, handle.Update)
	return svc
}

// registerProjectAdminPages 注册主题 / 站点设置页面（adminPages）与编辑器面板（workbenchPages）。
// 页面写操作的 Casbin 权限点路径与迁移前逐字一致；组为 nil 时跳过对应注册。
// SetupProjectPages 注册主题 / 站点设置页面（adminPages）与编辑器面板（workbenchPages）。
// 独立入口的原因与 content 同构：页面依赖的 page / block 契约晚于 project 装配。
func SetupProjectPages(adminPages, workbenchPages *gin.RouterGroup,
	svc projectcontract.ProjectService, pages pagecontract.PageService, blocks blockcontract.BlockService) {
	if adminPages != nil {
		themes := &themeAdminHandle{projects: svc, pages: pages, blocks: blocks}
		adminPages.GET("/themes", themes.ThemeManage)
		adminPages.POST("/themes/create", builtin.CasbinMiddlewareForPath("/api/theme/create"), themes.CreateTheme)
		adminPages.POST("/themes/activate", builtin.CasbinMiddlewareForPath("/api/theme/activate"), themes.ActivateTheme)
		adminPages.POST("/themes/delete", builtin.CasbinMiddlewareForPath("/api/theme/delete"), themes.DeleteTheme)
		adminPages.GET("/themes/settings", themes.ThemeSettings)
		adminPages.POST("/themes/settings/save", builtin.CasbinMiddlewareForPath("/api/theme/update"), themes.SaveThemeSettings)
		adminPages.GET("/theme", themes.ThemeRedirect)

		settings := &siteSettingsAdminHandle{projects: svc, pages: pages}
		adminPages.GET("/settings", settings.SiteSettings)
		adminPages.POST("/settings/save", builtin.CasbinMiddlewareForPath("/api/project/update"), settings.SaveSiteSettings)
		adminPages.POST("/settings/locales/rows", builtin.CasbinMiddlewareForPath("/api/project/update"), settings.LocaleRowsFragment)
		adminPages.POST("/settings/locales/save", builtin.CasbinMiddlewareForPath("/api/project/update"), settings.SaveSiteLocales)
	}
	if workbenchPages != nil {
		workbenchPages.POST("/workbench/settings", workbenchSettingsPanel)
		workbenchPages.POST("/workbench/seo-score-panel", workbenchSeoScorePanel)
		workbenchPages.POST("/workbench/global", workbenchGlobalPanel)
	}
}
