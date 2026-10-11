package pluginhttp

// plugin_page_handle.go — 后台插件管理页（/admin/plugins，Jet + HTMX，docs/06）。
// 自 dashboard 迁回本模块：页面路由（Session + CSRF）由装配层挂好，注册落点在
// plugin_page_router.go；页面 GET 按列表 API 鉴权，安装 POST 按安装 API 鉴权。
// 启停与卸载由浏览器直接调用现有 JSON API，失败留在列表页。
// 数据经 /api/plugin/* 业务 API（三层链）或本模块直调契约（页面渲染需要）。
//
// 失败出口统一走 plugin_err.go 的 pluginPageJump：渲染整页提示（文案走响应体），
// **不再** c.String 直出 `pluginenums.ErrXxx`（那会让浏览器停在 POST 路径上，
// 只剩一行英文标识符，连导航都没有）。
// 本模块 enums 的值保持 i18n key 形态（JSON 出口的形态判据要用），页面侧由出口翻译成中文。

import (
	"io"
	"net/http"

	"go_wp/internal/adminui"
	plugincontract "go_wp/internal/module/plugin/contract"
	pluginenums "go_wp/internal/module/plugin/enums"
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
	// 标题交给**取词链**：这里给 i18n key（enums 常量值就是 key），adminui.Prepare 的
	// injectI18n 对 data["title"] 做的是 t(title, title) —— 给 key 得到当前语言译文、
	// 给成品串原样返回。Go 侧先翻一遍再交出去是多余的（且要再想一遍「这句属于哪个语言」），
	// 与 page / block / contenttemplate 三页同一形态。
	data := &pluginsPageData{
		Title:          pluginenums.TitlePlugins,
		Menu:           "plugins",
		ArtifactPatrol: emptyPatrol(),
	}
	// 写动作的结论不在本页回显（走 shell.RenderJump 渲染提示页，见 plugin_err.go），
	// 所以提示条只由本页取数失败填充 —— 用户当下看到的是列表没加载出来。
	//
	// 这个槽位（模板 admin/plugin/plugins.html 的 {{if .Error}}）暂时把**事实取成文本**再传：
	// 它还没取词化（登记在批次报告里），但取词本身已走 adminui.MsgText，不再经 shell。
	if h.plugins != nil {
		list, err := h.plugins.List(c.Request.Context())
		if err != nil {
			data.Error = adminui.MsgText(c, pluginNoticeListFailed)
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
	c.HTML(http.StatusOK, "admin/plugin/plugins", adminui.Prepare(c, gin.H{
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
// 所以安装路径的每条文案都经 pluginInstallFailMsg 补一句「请重新选择文件」。
func (h *pluginPageHandle) PluginsInstall(c *gin.Context) {
	if h.plugins == nil {
		pluginPageJump(c, false, pluginNoticeModuleUnwired)
		return
	}
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		pluginPageJump(c, false, pluginInstallFailMsg(c, pluginNoticeNoFile))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, pluginsUploadMax+1))
	if err != nil || len(data) == 0 || len(data) > pluginsUploadMax {
		pluginPageJump(c, false, pluginInstallFailMsg(c, pluginNoticeUnreadable))
		return
	}
	if _, err := h.plugins.Install(c.Request.Context(), data); err != nil {
		logger.Scene("plugin").Error(err, "插件安装失败")
		pluginPageJump(c, false, pluginInstallFailMsg(c, pluginErrMsg(c, err)))
		return
	}
	pluginPageJump(c, true, pluginMsg{Key: pluginenums.MsgInstallSuccess, Fallback: "插件安装成功"})
}
