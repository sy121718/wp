package sysconfighttp

import (
	"github.com/gin-gonic/gin"
	pageview "go_wp/pkg/view"

	mw "go_wp/pkg/middleware"

	"go_wp/internal/adminui"
)

const SystemSettingsPageTemplate = "admin/system/settings"

func SetupSysConfigPages(adminPages *gin.RouterGroup, h *AdminHandle) {
	if adminPages == nil || h == nil {
		return
	}
	adminPages.GET("/system",
		mw.PageAuthz("/api/sysconfig/get"),
		pageview.Slot(SystemSettingsPageTemplate, adminui.RenderPage),
		h.SystemPage)
}
