package navigationhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	blockcontract "go_wp/internal/module/block/contract"
	navigationcontract "go_wp/internal/module/navigation/contract"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
	mw "go_wp/pkg/middleware"
	pageview "go_wp/pkg/view"
)

const NavigationEditFormTemplate = "admin/navigation/navigation_edit_form"

const NavigationPanelCreatedTemplate = "admin/navigation/panel_created"

const NavigationsPageTemplate = "admin/navigation/navigations"

const NavigationTranslationsTemplate = "admin/navigation/navigation_translations.html"

func SetupNavigationPages(reg *mw.Registry, adminPages *gin.RouterGroup,
	navigations navigationcontract.NavigationService,
	projects projectcontract.ProjectService, _ pagecontract.PageService,
	blocks blockcontract.BlockService) {
	if adminPages == nil {
		return
	}
	h := NewNavigationPageHandle(navigations, projects)

	h.SetBlockPanelPort(blocks)
	adminPages.GET("/navigations",
		mw.PageAuthz("/api/navigation/list"),
		pageview.Slot(NavigationsPageTemplate, pageview.RenderPage),
		h.NavigationsPage)
	mw.DeclarePageGetOn(reg, "/admin/navigations", "/api/navigation/list")

	adminPages.GET("/navigations/edit",
		builtin.CasbinMiddlewareForPathAs("/api/navigation/update", http.MethodPost),
		pageview.Slot(NavigationEditFormTemplate, pageview.RenderBare),
		h.NavigationEditFragment)
	mw.DeclarePageGetAsOn(reg, "/admin/navigations/edit", "/api/navigation/update", http.MethodPost)

	// 这三个是抽屉内的局部写：失败只进宿主声明的错误区（HTML 片段），
	// 不整页跳转、不返回 JSON，状态码保留真实语义。
	adminPages.POST("/navigations/panel",
		builtin.CasbinMiddlewareForPath("/api/navigation/update"), pageview.HTMLFragment, h.PanelSet)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/navigations/panel", "/api/navigation/update", http.MethodPost)
	}
	adminPages.POST("/navigations/panel/create",
		builtin.CasbinMiddlewareForPath("/api/block/create"), pageview.HTMLFragment, h.PanelCreate)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/navigations/panel/create", "/api/block/create", http.MethodPost)
	}
	adminPages.POST("/navigations/update",
		builtin.CasbinMiddlewareForPath("/api/navigation/update"), pageview.HTMLFragment, h.NavigationUpdate)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/navigations/update", "/api/navigation/update", http.MethodPost)
	}

	translations := NewNavigationTranslationHandle(navigations, projects)

	if writer, werr := i18n.NewContentWriterDefault(); werr == nil {
		translations.SetContentWriter(writer)
	}

	adminPages.GET("/navigations/translations",
		mw.PageAuthz("/api/navigation/list"),
		pageview.Slot(NavigationTranslationsTemplate, pageview.RenderPage),
		translations.NavigationTranslations)
	mw.DeclarePageGetOn(reg, "/admin/navigations/translations", "/api/navigation/list")
}
