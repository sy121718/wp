package pluginhttp

// plugin_page_handle.go — 后台插件管理页（/admin/plugins，Jet + HTMX，docs/06）。
// 自 dashboard 迁回本模块：页面路由（Session + CSRF，无 Casbin——页面路由约定）；
// 数据经 /api/plugin/* 业务 API（三层链）或本模块直调契约（页面渲染需要）。

import (
	"io"
	"net/http"

	"go_wp/internal/middleware/builtin"
	plugincontract "go_wp/internal/module/plugin/contract"
	pluginenums "go_wp/internal/module/plugin/enums"
	"go_wp/internal/web/shell"
	"go_wp/pkg/logger"

	"github.com/gin-gonic/gin"
)

// pluginsPageData 插件管理页数据。
type pluginsPageData struct {
	Title   string
	Menu    string
	Plugins []*plugincontract.PluginResp
	Error   string
}

// pluginPageHandle 插件管理页处理器。
type pluginPageHandle struct {
	plugins plugincontract.PluginService
}

// PluginsPage 插件管理列表页。
func (h *pluginPageHandle) PluginsPage(c *gin.Context) {
	data := &pluginsPageData{Title: "插件管理", Menu: "plugins"}
	if h.plugins != nil {
		list, err := h.plugins.List(c.Request.Context())
		if err != nil {
			data.Error = "插件列表加载失败"
		} else {
			data.Plugins = list
		}
	}
	c.HTML(http.StatusOK, "admin/plugins", shell.Prepare(c, gin.H{
		"title": data.Title, "menu": data.Menu,
		"Plugins": data.Plugins, "Error": data.Error,
	}))
}

// pluginsUploadMax 与安装防线对齐的上传上限。
const pluginsUploadMax = 52 << 20

// PluginsInstall 上传安装插件（multipart 表单，HTMX 提交）。
func (h *pluginPageHandle) PluginsInstall(c *gin.Context) {
	if h.plugins == nil {
		c.String(http.StatusServiceUnavailable, "插件模块未装配")
		return
	}
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		c.String(http.StatusBadRequest, pluginenums.ErrInstallParse)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, pluginsUploadMax+1))
	if err != nil || len(data) == 0 || len(data) > pluginsUploadMax {
		c.String(http.StatusBadRequest, pluginenums.ErrInstallParse)
		return
	}
	if _, err := h.plugins.Install(c.Request.Context(), data); err != nil {
		logger.Scene("plugin").Error(err, "插件安装失败")
		c.String(http.StatusBadRequest, pluginenums.MsgInstallFailed)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/plugins")
}

// PluginsToggle 启停插件（表单 POST）。
func (h *pluginPageHandle) PluginsToggle(c *gin.Context) {
	if h.plugins == nil {
		c.String(http.StatusServiceUnavailable, "插件模块未装配")
		return
	}
	req := &plugincontract.ToggleReq{ID: c.PostForm("id"), Enabled: c.PostForm("enabled") == "true" || c.PostForm("enabled") == "on"}
	if err := h.plugins.Toggle(c.Request.Context(), req); err != nil {
		logger.Scene("plugin").With("plugin_id", req.ID).Error(err, "插件状态更新失败")
		c.String(http.StatusBadRequest, pluginenums.ErrToggleFailed)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/plugins")
}

// PluginsUninstall 卸载插件（表单 POST，二次确认由前端 confirm 承担）。
func (h *pluginPageHandle) PluginsUninstall(c *gin.Context) {
	if h.plugins == nil {
		c.String(http.StatusServiceUnavailable, "插件模块未装配")
		return
	}
	req := &plugincontract.UninstallReq{ID: c.PostForm("id")}
	if err := h.plugins.Uninstall(c.Request.Context(), req); err != nil {
		logger.Scene("plugin").With("plugin_id", req.ID).Error(err, "插件卸载失败")
		c.String(http.StatusBadRequest, pluginenums.ErrUninstallFailed)
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/plugins")
}

// SetupPluginPages 注册插件管理页（/admin 组，中间件链由装配层统一挂好）。
// 函数名沿用 SetupXxxPages 先例：本包已有 REST 路由的 SetupPluginRoutes，不能同名。
// 插件安装/卸载/启停为写动作，权限点路径逐字保留（/api/plugin/*，迁移 032 seed）。
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
