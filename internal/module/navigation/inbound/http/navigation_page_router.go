package navigationhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	blockcontract "go_wp/internal/module/block/contract"
	navigationcontract "go_wp/internal/module/navigation/contract"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
)

const NavigationEditFormTemplate = "admin/navigation/navigation_edit_form"

const NavigationsPageTemplate = "admin/navigation/navigations"

const NavigationTranslationsTemplate = "admin/navigation/navigation_translations.html"

func SetupNavigationPages(adminPages *gin.RouterGroup,
	navigations navigationcontract.NavigationService,
	projects projectcontract.ProjectService, _ pagecontract.PageService,
	blocks blockcontract.BlockService) {
	if adminPages == nil {
		return
	}
	h := NewNavigationPageHandle(navigations, projects)

	h.SetBlockPanelPort(blocks)
	adminPages.GET("/navigations",
		adminui.PageAuthz("/api/navigation/list"),
		adminui.PageSlot(NavigationsPageTemplate),
		h.NavigationsPage)

	adminPages.GET("/navigations/edit",
		builtin.CasbinMiddlewareForPathAs("/api/navigation/update", http.MethodPost),
		adminui.BareSlot(NavigationEditFormTemplate),
		h.NavigationEditFragment)

	adminPages.POST("/navigations/panel", builtin.CasbinMiddlewareForPath("/api/navigation/update"), h.PanelSet)
	adminPages.POST("/navigations/panel/create", builtin.CasbinMiddlewareForPath("/api/block/create"), h.PanelCreate)
	adminPages.POST("/navigations/update", builtin.CasbinMiddlewareForPath("/api/navigation/update"), h.NavigationUpdate)

	translations := NewNavigationTranslationHandle(navigations, projects)

	if writer, werr := i18n.NewContentWriterDefault(); werr == nil {
		translations.SetContentWriter(writer)
	}

	adminPages.GET("/navigations/translations",
		adminui.PageAuthz("/api/navigation/list"),
		adminui.PageSlot(NavigationTranslationsTemplate),
		translations.NavigationTranslations)
}
