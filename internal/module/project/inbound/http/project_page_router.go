package projecthttp

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	blockcontract "go_wp/internal/module/block/contract"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	sysconfigcontract "go_wp/internal/module/sysconfig/contract"
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

func SetupProjectPages(adminPages, workbenchPages *gin.RouterGroup,
	svc projectcontract.ProjectService, pages pagecontract.PageService, blocks blockcontract.BlockService,
	dict sysconfigcontract.DictReader) {
	if adminPages != nil {
		themes := &themeAdminHandle{projects: svc, pages: pages, blocks: blocks}
		adminPages.GET("/themes",
			adminui.PageAuthz("/api/theme/list"),
			pageview.Slot(ThemeManagePageTemplate, adminui.RenderPage),
			themes.ThemeManage)
		adminPages.POST("/themes/create", builtin.CasbinMiddlewareForPath("/api/theme/create"), pageview.HTMLFragment, themes.CreateTheme)
		adminPages.POST("/themes/activate", builtin.CasbinMiddlewareForPath("/api/theme/activate"), pageview.HTMLFragment, themes.ActivateTheme)
		adminPages.POST("/themes/delete", builtin.CasbinMiddlewareForPath("/api/theme/delete"), pageview.HTMLFragment, themes.DeleteTheme)
		adminPages.GET("/themes/settings",
			adminui.PageAuthz("/api/theme/list"),
			pageview.Slot(ThemeSettingsPageTemplate, adminui.RenderPage),
			themes.ThemeSettings)
		adminPages.POST("/themes/settings/save", builtin.CasbinMiddlewareForPath("/api/theme/update"), pageview.HTMLFragment, themes.SaveThemeSettings)
		adminPages.GET("/theme", themes.ThemeRedirect)

		settings := &siteSettingsAdminHandle{projects: svc, pages: pages, dict: dict}
		adminPages.GET("/settings",
			adminui.PageAuthz("/api/project/detail"),
			pageview.Slot(SiteSettingsPageTemplate, adminui.RenderPage),
			settings.SiteSettings)
		adminPages.POST("/settings/save", builtin.CasbinMiddlewareForPath("/api/project/update"), pageview.HTMLFragment, settings.SaveSiteSettings)
		adminPages.POST("/settings/locales/rows", builtin.CasbinMiddlewareForPath("/api/project/update"),
			pageview.Slot(LocaleRowsFragmentTemplate, adminui.RenderBare), settings.LocaleRowsFragment)
		adminPages.POST("/settings/locales/save", builtin.CasbinMiddlewareForPath("/api/project/update"), pageview.HTMLFragment, settings.SaveSiteLocales)
	}
	if workbenchPages != nil {
		workbenchPages.POST("/workbench/settings",
			pageview.Slot(SettingsPanelFragmentTemplate, adminui.RenderBare), workbenchSettingsPanel)
		workbenchPages.POST("/workbench/seo-score-panel",
			pageview.Slot(SeoScoreFragmentTemplate, adminui.RenderBare), workbenchSeoScorePanel)
		workbenchPages.POST("/workbench/global",
			pageview.Slot(GlobalPanelFragmentTemplate, adminui.RenderBare), workbenchGlobalPanel)
	}
}
