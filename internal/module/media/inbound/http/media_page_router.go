package mediahttp

import (
	"github.com/gin-gonic/gin"
	pageview "go_wp/pkg/view"

	mw "go_wp/pkg/middleware"
)

const MediaPageTemplate = "admin/media/media"

func SetupMediaPages(reg *mw.Registry, adminPages *gin.RouterGroup) {
	if adminPages == nil {
		return
	}
	adminPages.GET("/media",
		mw.PageAuthz("/api/media/list"),
		pageview.Slot(MediaPageTemplate, pageview.RenderPage),
		MediaPage)
	mw.DeclarePageGetOn(reg, "/admin/media", "/api/media/list")
}
