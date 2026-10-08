package pluginhttp

// plugin_page_router.go — 后台插件管理页的注册落点。
//
// 只做注册：`/admin` 组的中间件链（Session + CSRF + 权限上下文）由装配层统一挂好，
// handler 在 plugin_page_handle.go。路由注册只出现在 *_router.go，门禁
// scripts/check-route-registration-placement.sh 守这条。
//
// 写动作按各自业务 API 的权限点 enforce（`/api/plugin/*`，迁移 032 seed）——
// 页面路径本身不在权限点表里，直接以页面路径 enforce 会让所有人（含超管）403。

import (
	"go_wp/internal/middleware/builtin"
	plugincontract "go_wp/internal/module/plugin/contract"

	"github.com/gin-gonic/gin"
)

// SetupPluginPages 注册插件管理页（/admin 组，中间件链由装配层统一挂好）。
// 函数名沿用 SetupXxxPages 先例：本包已有 REST 路由的 SetupPluginRoutes，不能同名。
// adminPages 为 nil 时整体跳过。
func SetupPluginPages(adminPages *gin.RouterGroup, plugins plugincontract.PluginService) {
	if adminPages == nil {
		return
	}
	h := &pluginPageHandle{plugins: plugins}
	adminPages.GET("/plugins", h.PluginsPage)
	adminPages.POST("/plugins/install", builtin.CasbinMiddlewareForPath("/api/plugin/install"), h.PluginsInstall)
	adminPages.POST("/plugins/toggle", builtin.CasbinMiddlewareForPath("/api/plugin/toggle"), h.PluginsToggle)
	adminPages.POST("/plugins/uninstall", builtin.CasbinMiddlewareForPath("/api/plugin/uninstall"), h.PluginsUninstall)
}
