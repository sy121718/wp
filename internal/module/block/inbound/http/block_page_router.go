package blockhttp

import (
	"github.com/gin-gonic/gin"
	pageview "go_wp/pkg/view"
	"net/http"

	"go_wp/internal/middleware/builtin"
	blockcontract "go_wp/internal/module/block/contract"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	mw "go_wp/pkg/middleware"
)

const StalePagesDrawerTemplate = "admin/partials/stale_pages_drawer.html"

const BlocksPageTemplate = "admin/block/blocks"

func SetupBlockPages(reg *mw.Registry, adminPages *gin.RouterGroup,
	blocks blockcontract.BlockService, projects projectcontract.ProjectService,
	pages ...pagecontract.PageService) {
	if adminPages == nil {
		return
	}
	h := NewBlockPageHandle(blocks, projects, pages...)

	adminPages.GET("/blocks",
		mw.PageAuthz("/api/block/list"),
		pageview.Slot(BlocksPageTemplate, pageview.RenderPage),
		h.BlocksList)
	mw.DeclarePageGetOn(reg, "/admin/blocks", "/api/block/list")

	adminPages.GET("/blocks/stale/drawer",
		builtin.CasbinMiddlewareForPath("/api/block/list"),
		pageview.Slot(StalePagesDrawerTemplate, pageview.RenderBare),
		h.BlocksStaleDrawer)
	mw.DeclarePageGetOn(reg, "/admin/blocks/stale/drawer", "/api/block/list")

	adminPages.POST("/blocks/save-content", builtin.CasbinMiddlewareForPath("/api/block/update"), h.SaveBlockContent)
	if reg != nil {
		reg.DeclarePageWrite(http.MethodPost, "/admin/blocks/save-content", "/api/block/update", http.MethodPost)
	}

}
