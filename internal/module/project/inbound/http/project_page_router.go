package projecthttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	blockcontract "go_wp/internal/module/block/contract"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
	mw "go_wp/pkg/middleware"
	pageview "go_wp/pkg/view"
)

const (
	ThemeManagePageTemplate   = "admin/project/theme"
	ThemeSettingsPageTemplate = "admin/project/theme_settings"
	SiteSettingsPageTemplate  = "admin/project/settings"

	LocaleRowsFragmentTemplate    = "admin/partials/locale_rows"
	GlobalPanelFragmentTemplate   = "fragments/global_panel"
	SettingsPanelFragmentTemplate = "fragments/settings_panel"
	SeoScoreFragmentTemplate      = "fragments/seo_score"
)

func SetupProjectPages(reg *mw.Registry, adminPages, workbenchPages *gin.RouterGroup,
	svc projectcontract.ProjectService, pages pagecontract.PageService, blocks blockcontract.BlockService,
	dict sysconfigcontract.DictReader) {
	if adminPages != nil {
		themes := &themeAdminHandle{projects: svc, pages: pages, blocks: blocks}
		adminPages.GET("/themes",
			mw.PageAuthz("/api/theme/list"),
			pageview.Slot(ThemeManagePageTemplate, pageview.RenderPage),
			themes.ThemeManage)
		mw.DeclarePageGetOn(reg, "/admin/themes", "/api/theme/list")
		adminPages.POST("/themes/create", builtin.CasbinMiddlewareForPath("/api/theme/create"), pageview.HTMLFragment, themes.CreateTheme)
		mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/themes/create", "/api/theme/create", http.MethodPost)
		adminPages.POST("/themes/activate", builtin.CasbinMiddlewareForPath("/api/theme/activate"), pageview.HTMLFragment, themes.ActivateTheme)
		mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/themes/activate", "/api/theme/activate", http.MethodPost)
		adminPages.POST("/themes/delete", builtin.CasbinMiddlewareForPath("/api/theme/delete"), pageview.HTMLFragment, themes.DeleteTheme)
		mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/themes/delete", "/api/theme/delete", http.MethodPost)
		adminPages.GET("/themes/settings",
			mw.PageAuthz("/api/theme/list"),
			pageview.Slot(ThemeSettingsPageTemplate, pageview.RenderPage),
			themes.ThemeSettings)
		mw.DeclarePageGetOn(reg, "/admin/themes/settings", "/api/theme/list")
		// 保存失败要**就地重渲**同一个页面（带回全部已填值与字段级错误），所以这条
		// 写入口必须有渲染器；只声明 HTMLFragment 时 pageview.Render 找不到出口，
		// 校验失败会退化成 500，用户既看不到原因也拿不回输入。
		adminPages.POST("/themes/settings/save", builtin.CasbinMiddlewareForPath("/api/theme/update"),
			pageview.HTMLFragment, pageview.Slot(ThemeSettingsPageTemplate, pageview.RenderPage), themes.SaveThemeSettings)
		mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/themes/settings/save", "/api/theme/update", http.MethodPost)
		adminPages.GET("/theme", themes.ThemeRedirect)
		mw.DeclarePageGetOn(reg, "/admin/theme", "/api/theme/list")

		settings := &siteSettingsAdminHandle{projects: svc, pages: pages, dict: dict}
		adminPages.GET("/settings",
			mw.PageAuthz("/api/project/detail"),
			pageview.Slot(SiteSettingsPageTemplate, pageview.RenderPage),
			settings.SiteSettings)
		mw.DeclarePageGetOn(reg, "/admin/settings", "/api/project/detail")
		adminPages.POST("/settings/save", builtin.CasbinMiddlewareForPath("/api/project/update"), pageview.HTMLFragment, settings.SaveSiteSettings)
		mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/settings/save", "/api/project/update", http.MethodPost)
		adminPages.POST("/settings/locales/rows", builtin.CasbinMiddlewareForPath("/api/project/update"),
			pageview.Slot(LocaleRowsFragmentTemplate, pageview.RenderBare), settings.LocaleRowsFragment)
		mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/settings/locales/rows", "/api/project/update", http.MethodPost)
		adminPages.POST("/settings/locales/save", builtin.CasbinMiddlewareForPath("/api/project/update"), pageview.HTMLFragment, settings.SaveSiteLocales)
		mw.DeclarePageWriteOn(reg, http.MethodPost, "/admin/settings/locales/save", "/api/project/update", http.MethodPost)
	}
	if workbenchPages != nil {
		// 登记与**实际守卫**必须成对出现：父组只提供会话/CSRF/权限上下文，
		// 不代替逐操作鉴权。三块面板读的是站点设置文档，守卫用项目更新对象。
		// 失败也要按已经声明的 HTML 片段协议输出：缺 HTMLFragment 标记时失败出口会
		// 当成数据端点回 JSON 500，面板拿到的是无法渲染的对象而不是可读错误区。
		workbenchPages.POST("/workbench/settings", builtin.CasbinMiddlewareForPath("/api/project/update"),
			pageview.HTMLFragment, pageview.Slot(SettingsPanelFragmentTemplate, pageview.RenderBare), workbenchSettingsPanel)
		mw.DeclarePageWriteOn(reg, http.MethodPost, "/workbench/settings", "/api/project/update", http.MethodPost)
		workbenchPages.POST("/workbench/seo-score-panel", builtin.CasbinMiddlewareForPath("/api/project/update"),
			pageview.HTMLFragment, pageview.Slot(SeoScoreFragmentTemplate, pageview.RenderBare), workbenchSeoScorePanel)
		mw.DeclarePageWriteOn(reg, http.MethodPost, "/workbench/seo-score-panel", "/api/project/update", http.MethodPost)
		workbenchPages.POST("/workbench/global", builtin.CasbinMiddlewareForPath("/api/project/update"),
			pageview.HTMLFragment, pageview.Slot(GlobalPanelFragmentTemplate, pageview.RenderBare), workbenchGlobalPanel)
		mw.DeclarePageWriteOn(reg, http.MethodPost, "/workbench/global", "/api/project/update", http.MethodPost)
	}
}
