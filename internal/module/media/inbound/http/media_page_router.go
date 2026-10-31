package mediahttp

import (
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
		adminui.PageSlot(MediaPageTemplate),
		MediaPage)
}
