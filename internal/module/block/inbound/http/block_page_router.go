package blockhttp

import (
	"github.com/gin-gonic/gin"
	pageview "go_wp/pkg/view"

	mw "go_wp/pkg/middleware"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	blockcontract "go_wp/internal/module/block/contract"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
)

const StalePagesDrawerTemplate = "admin/partials/stale_pages_drawer.html"

const BlocksPageTemplate = "admin/block/blocks"

func SetupBlockPages(adminPages *gin.RouterGroup,
	blocks blockcontract.BlockService, projects projectcontract.ProjectService,
	pages ...pagecontract.PageService) {
	if adminPages == nil {
		return
	}
	h := NewBlockPageHandle(blocks, projects, pages...)

	adminPages.GET("/blocks",
		mw.PageAuthz("/api/block/list"),
		pageview.Slot(BlocksPageTemplate, adminui.RenderPage),
		h.BlocksList)

	adminPages.GET("/blocks/stale/drawer",
		builtin.CasbinMiddlewareForPath("/api/block/list"),
		pageview.Slot(StalePagesDrawerTemplate, adminui.RenderBare),
		h.BlocksStaleDrawer)

	adminPages.POST("/blocks/save-content", builtin.CasbinMiddlewareForPath("/api/block/update"), h.SaveBlockContent)
}
