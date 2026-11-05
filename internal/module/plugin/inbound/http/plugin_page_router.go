package pluginhttp

import (
	pageview "go_wp/pkg/view"
	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	plugincontract "go_wp/internal/module/plugin/contract"

	"github.com/gin-gonic/gin"
)

const PluginsPageTemplate = "admin/plugin/plugins"

func SetupPluginPages(adminPages *gin.RouterGroup, plugins plugincontract.PluginService) {
	if adminPages == nil {
		return
	}
	h := &pluginPageHandle{plugins: plugins}
	adminPages.GET("/plugins",
		adminui.PageAuthz("/api/plugin/list"),
		pageview.Slot(PluginsPageTemplate, adminui.RenderPage),
		h.PluginsPage)
	adminPages.POST("/plugins/install", builtin.CasbinMiddlewareForPath("/api/plugin/install"), h.PluginsInstall)
}
