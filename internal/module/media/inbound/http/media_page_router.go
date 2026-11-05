package mediahttp

import (
	pageview "go_wp/pkg/view"
	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
)

const MediaPageTemplate = "admin/media/media"

func SetupMediaPages(adminPages *gin.RouterGroup) {
	if adminPages == nil {
		return
	}
	adminPages.GET("/media",
		adminui.PageAuthz("/api/media/list"),
		pageview.Slot(MediaPageTemplate, adminui.RenderPage),
		MediaPage)
}
