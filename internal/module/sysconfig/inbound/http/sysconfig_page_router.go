package sysconfighttp

import (
	pageview "go_wp/pkg/view"
	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
)

const SystemSettingsPageTemplate = "admin/system/settings"

func SetupSysConfigPages(adminPages *gin.RouterGroup, h *AdminHandle) {
	if adminPages == nil || h == nil {
		return
	}
	adminPages.GET("/system",
		adminui.PageAuthz("/api/sysconfig/get"),
		pageview.Slot(SystemSettingsPageTemplate, adminui.RenderPage),
		h.SystemPage)
}
