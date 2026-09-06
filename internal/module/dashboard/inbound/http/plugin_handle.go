package dashboardhttp

// plugin_handle.go — 后台插件管理页（/admin/plugins，Jet + HTMX，docs/06）。
// 页面路由（Session + CSRF，无 Casbin——页面路由约定）；数据经 /api/plugin/*
// 业务 API（三层链）或本模块直调契约（页面渲染需要）。

import (
	"io"
	"net/http"

	plugindto "go_wp/internal/module/plugin/dto"
	pluginenums "go_wp/internal/module/plugin/enums"

	"github.com/gin-gonic/gin"
)

// pluginsPageData 插件管理页数据。
type pluginsPageData struct {
	Title   string
	Menu    string
	Plugins []*plugindto.PluginResp
	Error   string
}

// PluginsPage 插件管理列表页。
func (h *Handle) PluginsPage(c *gin.Context) {
	data := &pluginsPageData{Title: "插件管理", Menu: "plugins"}
	if h.plugins != nil {
		list, err := h.plugins.List(c.Request.Context())
		if err != nil {
			data.Error = "插件列表加载失败"
		} else {
			data.Plugins = list
		}
	}
	c.HTML(http.StatusOK, "admin/plugins", withCSRF(c, gin.H{
		"title": data.Title, "menu": data.Menu,
		"Plugins": data.Plugins, "Error": data.Error,
	}))
}

// pluginsUploadMax 与安装防线对齐的上传上限。
const pluginsUploadMax = 52 << 20

// PluginsInstall 上传安装插件（multipart 表单，HTMX 提交）。
func (h *Handle) PluginsInstall(c *gin.Context) {
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
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/plugins")
}

// PluginsToggle 启停插件（表单 POST）。
func (h *Handle) PluginsToggle(c *gin.Context) {
	if h.plugins == nil {
		c.String(http.StatusServiceUnavailable, "插件模块未装配")
		return
	}
	req := &plugindto.ToggleReq{ID: c.PostForm("id"), Enabled: c.PostForm("enabled") == "true" || c.PostForm("enabled") == "on"}
	if err := h.plugins.Toggle(c.Request.Context(), req); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/plugins")
}

// PluginsUninstall 卸载插件（表单 POST，二次确认由前端 confirm 承担）。
func (h *Handle) PluginsUninstall(c *gin.Context) {
	if h.plugins == nil {
		c.String(http.StatusServiceUnavailable, "插件模块未装配")
		return
	}
	req := &plugindto.UninstallReq{ID: c.PostForm("id")}
	if err := h.plugins.Uninstall(c.Request.Context(), req); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	c.Redirect(http.StatusSeeOther, "/admin/plugins")
}
