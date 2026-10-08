package pluginhttp

// plugin_page_handle.go — 后台插件管理页（/admin/plugins，Jet + HTMX，docs/06）。
// 自 dashboard 迁回本模块：页面路由（Session + CSRF）由装配层挂好，注册落点在
// plugin_page_router.go；页面 GET 的 Casbin 待补（见 docs/02-Z-admin-menu-code-and-page-authz.md §4.3），
// 写动作已按 /api/plugin/* 的权限点 enforce。
// 数据经 /api/plugin/* 业务 API（三层链）或本模块直调契约（页面渲染需要）。
//
// 失败出口统一走 plugin_err.go 的 pluginPageJump：渲染整页提示（文案走响应体），
// **不再** c.String 直出 `pluginenums.ErrXxx`（那会让浏览器停在 POST 路径上，
// 只剩一行英文标识符，连导航都没有）。
// 本模块 enums 的值保持 i18n key 形态（JSON 出口的形态判据要用），页面侧由出口翻译成中文。

import (
	"io"
	"net/http"

	plugincontract "go_wp/internal/module/plugin/contract"
	pluginenums "go_wp/internal/module/plugin/enums"
	"go_wp/internal/shell"
	"go_wp/pkg/logger"

	"github.com/gin-gonic/gin"
)

// pluginsPageData 插件管理页数据。
type pluginsPageData struct {
	Title   string
	Menu    string
	Plugins []*plugincontract.PluginResp
	// Error 页面提示条（模板 admin/plugins.html:21-23 的 {{if .Error}}）：
	// 只剩一处来源 —— 列表取数失败的固定文案。写动作的结论不再回带（走 shell.RenderJump）。
	Error string
	// ArtifactPatrol 插件三处产物的对账巡检结果（只读）。见 service/plugin_patrol.go：
	// 报告孤儿 schema / 缺 schema 的注册行 / 孤儿存储目录 / 目录缺失的注册行四类不一致。
	// 恒定非 nil：模板按「有没有不一致」分流，nil 会让它多一个判空分支。
	ArtifactPatrol *plugincontract.PatrolResp
}

// pluginPageHandle 插件管理页处理器。
type pluginPageHandle struct {
	plugins plugincontract.PluginService
}

// emptyPatrol 巡检的空结果（非 nil）。
//
// 巡检失败时**不**把页面打成错误页：它是只读附加信息，挂了不该连带插件列表一起看不见。
// 但也不能让模板拿到 nil 指针（Jet 取 nil 的字段会中断整页渲染，HTTP 仍 200 ——
// 见 internal/templates/CLAUDE.md），所以失败时给这个空值。
func emptyPatrol() *plugincontract.PatrolResp {
	return &plugincontract.PatrolResp{
		OrphanSchemas:  []plugincontract.SchemaInfo{},
		MissingSchemas: []string{},
		OrphanStorage:  []string{},
		MissingStorage: []string{},
	}
}

// PluginsPage 插件管理列表页。
func (h *pluginPageHandle) PluginsPage(c *gin.Context) {
	// 标题在这里就翻成当前语言：shell.Prepare 对 data 的 title 做的是 t(title, title)，
	// 已翻译的值不是 key、会原样返回（见 internal/shell/shell.go）。写成裸中文的话，
	// 英文站点的浏览器标题与面包屑恒为中文。
	data := &pluginsPageData{
		Title:          shell.TranslateFor(c)(pluginenums.TitlePlugins, "插件管理"),
		Menu:           "plugins",
		ArtifactPatrol: emptyPatrol(),
	}
	// 写动作的结论不在本页回显（走 shell.RenderJump 渲染提示页，见 plugin_err.go），
	// 所以提示条只由本页取数失败填充 —— 用户当下看到的是列表没加载出来。
	if h.plugins != nil {
		list, err := h.plugins.List(c.Request.Context())
		if err != nil {
			data.Error = pluginFacingText(c, pluginNoticeListFailed)
		} else {
			data.Plugins = list
		}
		// 只读巡检：失败只记日志降级为空报告，不影响列表与安装入口。
		if patrol, perr := h.plugins.PatrolArtifacts(c.Request.Context()); perr != nil {
			logger.Scene("plugin").Error(perr, "插件产物巡检失败")
		} else if patrol != nil {
			data.ArtifactPatrol = patrol
		}
	}
	c.HTML(http.StatusOK, "admin/plugin/plugins", shell.Prepare(c, gin.H{
		"title": data.Title, "menu": data.Menu,
		"Plugins": data.Plugins, "Error": data.Error,
		"ArtifactPatrol": data.ArtifactPatrol,
	}))
}

// pluginsUploadMax 与安装防线对齐的上传上限。
const pluginsUploadMax = 52 << 20

// PluginsInstall 上传安装插件（multipart 表单，HTMX 提交）。
//
// 失败一律走 pluginPageJump 渲染提示页（见 plugin_err.go）：直出 c.String 时浏览器停在
// POST 路径上，用户看到的是 `ErrInstallParse` 这样一行英文标识符，而**已选的文件也白选了** ——
// 所以安装路径的每条文案都经 pluginInstallFailText 补一句「请重新选择文件」。
func (h *pluginPageHandle) PluginsInstall(c *gin.Context) {
	if h.plugins == nil {
		pluginPageJump(c, false, pluginFacingText(c, pluginNoticeModuleUnwired))
		return
	}
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		pluginPageJump(c, false, pluginInstallFailText(c, pluginFacingText(c, pluginNoticeNoFile)))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, pluginsUploadMax+1))
	if err != nil || len(data) == 0 || len(data) > pluginsUploadMax {
		pluginPageJump(c, false, pluginInstallFailText(c, pluginFacingText(c, pluginNoticeUnreadable)))
		return
	}
	if _, err := h.plugins.Install(c.Request.Context(), data); err != nil {
		logger.Scene("plugin").Error(err, "插件安装失败")
		pluginPageJump(c, false, pluginInstallFailText(c, pluginErrParam(c, err)))
		return
	}
	pluginPageJump(c, true, shell.TranslateFor(c)(pluginenums.MsgInstallSuccess, "插件安装成功"))
}

// PluginsToggle 启停插件（表单 POST）。
//
// 表单提交失败必须回到**页面**：c.String 的响应体在浏览器里就是一行英文标识符，
// 没有导航也没有返回，用户只能按后退键（而 POST 之后的后退会重发表单）。
func (h *pluginPageHandle) PluginsToggle(c *gin.Context) {
	if h.plugins == nil {
		pluginPageJump(c, false, pluginFacingText(c, pluginNoticeModuleUnwired))
		return
	}
	req := &plugincontract.ToggleReq{ID: c.PostForm("id"), Enabled: c.PostForm("enabled") == "true" || c.PostForm("enabled") == "on"}
	if err := h.plugins.Toggle(c.Request.Context(), req); err != nil {
		logger.Scene("plugin").With("plugin_id", req.ID).Error(err, "插件状态更新失败")
		pluginPageJump(c, false, pluginErrParam(c, err))
		return
	}
	pluginPageJump(c, true, shell.TranslateFor(c)(pluginenums.MsgToggleSuccess, "插件状态更新成功"))
}

// PluginsUninstall 卸载插件（表单 POST，二次确认由前端 confirm 承担）。
func (h *pluginPageHandle) PluginsUninstall(c *gin.Context) {
	if h.plugins == nil {
		pluginPageJump(c, false, pluginFacingText(c, pluginNoticeModuleUnwired))
		return
	}
	req := &plugincontract.UninstallReq{ID: c.PostForm("id")}
	if err := h.plugins.Uninstall(c.Request.Context(), req); err != nil {
		logger.Scene("plugin").With("plugin_id", req.ID).Error(err, "插件卸载失败")
		pluginPageJump(c, false, pluginErrParam(c, err))
		return
	}
	pluginPageJump(c, true, shell.TranslateFor(c)(pluginenums.MsgUninstallSuccess, "插件已卸载"))
}
