package workbenchhttp

import (
	_ "embed"
	"go_wp/pkg/binding"
	json "go_wp/pkg/jsonx"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	workbenchdto "go_wp/internal/module/workbench/dto"
	workbenchenums "go_wp/internal/module/workbench/enums"
	workbenchservice "go_wp/internal/module/workbench/service"
	"go_wp/internal/templates"
	"go_wp/pkg/response"
)

// InspectorPanel 渲染选中节点的检查器面板片段。
func (h *Handle) InspectorPanel(c *gin.Context) {
	req, err := binding.ParseForm[workbenchdto.InspectorPanelReq](c)
	if err != nil {
		c.String(http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrPreviewParamsIncomplete))
		return
	}
	nodeID := strings.TrimSpace(req.NodeID)
	node, err := workbenchservice.FindDocNode(json.RawMessage(req.Document), nodeID)
	if err != nil || node == nil {
		// 空态分支同样经 PrepareBare：片段模板里的取词（workbench.ui.inspector.empty 等）
		// 靠它注入的 t —— 漏注入时 Jet **静默**输出空串（不是报错、状态码 200、日志干净），
		// 空面板会变成一句话都没有。两条分支走同一个入口，不各写一份。
		c.HTML(http.StatusOK, "fragments/inspector_panel",
			adminui.PrepareBareView(c, &workbenchdto.InspectorPanelResp{NodeID: ""}))
		return
	}
	// tab：content / style（空 = 渲染全部，向后兼容旧调用）。
	tab := strings.TrimSpace(req.Tab)
	projectID := strings.TrimSpace(req.ProjectID)
	// 装配链（组件 schema 取数 → 解包 → 字段构造 → 分组桶 → 重复项面板）全在
	// service.InspectorSections：任一步取数失败都以 error 出来，本函数只决定出口形态
	//（保持 text/plain 不变 —— 前端是 fetch → r.text() → morphHTML，不检查 r.ok，
	// 换成 JSON 反而会把一段 JSON 铺进面板）。
	sections, err := h.svc.InspectorSections(c.Request.Context(), node, tab, projectID, workbenchTrFunc(c))
	if err != nil {
		c.String(http.StatusInternalServerError, adminui.PageInternalText(c))
		return
	}
	// 取词函数由 adminui.PrepareBare 注入（片段不经 Prepare 页壳，见该函数注释）：
	// 模板里的 workbench.ui.inspector.* 由渲染期按请求语言取，控制器不再提前取词。
	// 注意本片段的**面板正文**仍是 service 拼串产出的 HTML（Sections[].HTML / Title 等），
	// 那部分的取词留在 service —— 它没有模板槽位可放（见 workbench/service 文件头）。
	c.HTML(http.StatusOK, "fragments/inspector_panel",
		adminui.PrepareBareView(c, &workbenchdto.InspectorPanelResp{
			NodeID: nodeID, NodeType: node.Type,
			Sections: sections,
		}))
}

// OutlineTree 渲染结构树片段（过滤规则与前端一致：节点或任一后代命中即保留整条链路）。
func (h *Handle) OutlineTree(c *gin.Context) {
	var page struct {
		Root []workbenchservice.OutlineNode `json:"root"`
	}
	req, _ := binding.ParseForm[workbenchdto.OutlineTreeReq](c)
	if doc := req.Document; doc != "" {
		_ = json.Unmarshal([]byte(doc), &page)
	}
	selectedID := strings.TrimSpace(req.SelectedID)
	filter := strings.ToLower(strings.TrimSpace(req.Filter))
	// PrepareBare：与同目录另一个片段（inspector_panel）走同一个入口，片段自带取词函数
	// 才有统一的形状。**本片段今天并不消费 t/tf** —— 树 HTML 整块由
	// workbenchservice.RenderOutlineHTML 拼串产出（Go 拼串，取词在 service 侧经 tr 完成），
	// 模板只有一句 {{ .HTML | unsafe }}。注入在这里是为了「模板侧新增取词」时不必再想起它。
	c.HTML(http.StatusOK, "fragments/outline_tree",
		adminui.PrepareBareView(c, &workbenchdto.OutlineTreeResp{
			HTML: workbenchservice.RenderOutlineHTML(page.Root, selectedID, filter, workbenchTrFunc(c)),
		}))
}

// workbench_handle.go - 工作台页面与画布（编辑器壳 / 块与模板画布 / 模板预览）。

// renderWorkbenchPageError 工作台「整页打不开」的出口：回**完整外壳**，而不是一行裸文本。
//
// 为什么必须完整外壳：调用它的是**四种画布模式**的页面 handler（?id= 页面 / ?block= 全局块 /
// ?template= 内容模板 / ?instance= 实例），地址栏里都是一个页面 URL —— 回 text/plain 等于把
// 作者丢在一片空白上，连「返回后台」都没有；
// 契约见 docs/rules/template-boundary.md「整页 GET 的失败出口：失败也必须是「这一页」」。
//
// 状态码保留**事实区分**（400 请求缺参 / 404 目标不存在 / 500 未知内部异常），文案同源：
// title 与 message 都取自 workbenchShortFallbacks 的白名单短句（未登记的 key 拼不出来），
// 原文只进日志 —— 与 workbench_err.go 的①白名单②归口文案同一口径。
//
// 为什么不复用 workbench/layout：那个外壳用点号取大量编辑器专用键（.jsVer / .document /
// .schemas / .isBlock / .previewQS …），错误请求下这些值都不存在 —— 拿空值渲染等于把
// 「页面打不开」换成又一个渲染期 500。错误外壳因此是**独立完整页**（workbench/error.html），
// 只读本函数给的那几个键。
//
// 取词由 i18n.TranslateFunc 按 response.RequestLanguage 生成成品文案。
//
// no-store：失败页随后可能变成成功页（补上 id 再进来就是编辑器），缓存住会让浏览器
// 继续显示旧的失败页 —— 与 page/navigation 的整页 500 同口径。
func renderWorkbenchPageError(c *gin.Context, status int, key string) {
	renderWorkbenchPageErrorText(c, status, key, workbenchShortText(c, key))
}

// renderWorkbenchPageErrorText 与 renderWorkbenchPageError 是同一条出口，区别只在文案来源：
// 调用方已经把这一句算好了（占位符替换过的、或走 facing 白名单的），这里不再取词。
//
// 为什么需要这个变体（两处调用方都不是「key → 短句」能表达的）：
//   - 模板模式 400：词条里带 {type} 占位符，替换必须在取词**之后**做 —— 译文里 {type}
//     的位置与中文不同，在代码里拼句会拼出半句外语；
//   - 实例模式 422：文案走 workbenchFacingText（受控出口，未登记返回空串），
//     它是「这个实例还不能编辑」的业务状态，不属于 workbenchShortFallbacks 那张短句表。
//
// text 为空时回落 workbenchShortText(c, key)：空文案的失败页只剩顶栏与一个返回按钮，
// 与「页面打不开」这个事实不成比例（未登记的 key 也在这一步被兜住，并由短句表记日志）。
func renderWorkbenchPageErrorText(c *gin.Context, status int, key, text string) {
	if strings.TrimSpace(text) == "" {
		text = workbenchShortText(c, key)
	}
	lang := response.RequestLanguage(c)
	c.Header("Cache-Control", "no-store")
	c.HTML(status, "workbench/error", gin.H{
		"lang":     lang,
		"t":        templates.TranslateFunc(lang),
		"title":    text,
		"message":  text,
		"backPath": "/admin",
	})
}
