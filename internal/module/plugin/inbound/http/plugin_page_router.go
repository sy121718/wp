package pluginhttp

import (
	"go_wp/internal/middleware/builtin"
	plugincontract "go_wp/internal/module/plugin/contract"
	mw "go_wp/pkg/middleware"
	pageview "go_wp/pkg/view"

	"github.com/gin-gonic/gin"
)

const PluginsPageTemplate = "admin/plugin/plugins"

func SetupPluginPages(adminPages *gin.RouterGroup, plugins plugincontract.PluginService) {
	if adminPages == nil {
		return
	}
	h := &pluginPageHandle{plugins: plugins}
	adminPages.GET("/plugins",
		mw.PageAuthz("/api/plugin/list"),
		pageview.Slot(PluginsPageTemplate, pageview.RenderPage),
		h.PluginsPage)
	adminPages.POST("/plugins/install", builtin.CasbinMiddlewareForPath("/api/plugin/install"), h.PluginsInstall)
}
