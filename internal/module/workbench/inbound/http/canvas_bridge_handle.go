package workbenchhttp

import (
	_ "embed"
	"go_wp/pkg/binding"
	json "go_wp/pkg/jsonx"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	workbenchdto "go_wp/internal/module/workbench/dto"
	workbenchenums "go_wp/internal/module/workbench/enums"
	workbenchservice "go_wp/internal/module/workbench/service"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
)

// Preview 基于已保存草稿轻量编译并在独立响应中输出完整 HTML 文档
// （0-A1 §4.2 隔离预览：不落盘、不影响线上产物）。
func (h *Handle) Preview(c *gin.Context) {
	pageID := strings.TrimSpace(c.Query("id"))
	if pageID == "" {
		c.String(http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrMissingPageID))
		return
	}
	page, err := h.svc.PageByID(c.Request.Context(), pageID)
	if err != nil {
		c.String(http.StatusNotFound, workbenchShortText(c, workbenchenums.ErrPageNotFound))
		return
	}
	h.renderPreview(c, page.DraftDocument, page.ProjectID, page.DraftPath, c.Query("editor") == "1", workbenchservice.PreviewDocPage)
}

// BlockPreview 全局块画布预览（工作台块编辑模式 iframe 内嵌）。
func (h *Handle) BlockPreview(c *gin.Context) {
	blockID := strings.TrimSpace(c.Query("id"))
	if blockID == "" {
		c.String(http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrMissingBlockID))
		return
	}
	block, err := h.svc.BlockByID(c.Request.Context(), blockID)
	if err != nil || block == nil {
		c.String(http.StatusNotFound, workbenchShortText(c, workbenchenums.ErrBlockNotFound))
		return
	}
	h.renderPreview(c, block.Document, block.ProjectID, "", c.Query("editor") == "1", workbenchservice.PreviewDocBlock)
}

// PreviewDraft 基于未保存 AST 返回临时预览，不持久化、不写 Artifact、不影响发布指针。
func (h *Handle) PreviewDraft(c *gin.Context) {
	req, _ := binding.ParseForm[workbenchdto.PreviewDraftReq](c)
	pageID := strings.TrimSpace(req.ID)
	document := json.RawMessage(req.DraftDocument)
	version, err := strconv.ParseInt(strings.TrimSpace(req.ExpectedVersion), 10, 64)
	if pageID == "" || err != nil || len(document) == 0 {
		c.String(http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrDraftDecodeFailed))
		return
	}
	page, err := h.svc.PageByID(c.Request.Context(), pageID)
	if err != nil {
		c.String(http.StatusNotFound, workbenchShortText(c, workbenchenums.ErrPageNotFound))
		return
	}
	if version != page.DraftVersion {
		c.String(http.StatusConflict, workbenchShortText(c, workbenchenums.ErrDraftVersionStale))
		return
	}
	h.renderPreview(c, document, page.ProjectID, page.DraftPath, true, workbenchservice.PreviewDocPage)
}

// renderPreview 只完成 AST 校验与编译，响应生命周期结束即丢弃结果。
// 编译复用 page 模块 CompilePreview（与正式构建同源装配管线，AGENTS.md §10）：
// 全局块引用展开、插件组件集注入、主题/集合解析均与构建一致，画布所见即产物。
// editorBridge（画布联动 JS）为后处理拼接，与编译无关，仅预览启用。
// projectID 为文档所属站点工程（页面/块的记录字段），驱动导航等站点级资源解析；
// currentPath 为页面访问路径（导航当前项高亮，块预览传空）。
// kind 标识预览文档的来源（页面 / 全局块 / 结构模板）：422 的可归因文案按它分流，
// 因为同一句「编译失败」在三种画布上的下一步动作并不相同（见 workbench_err.go）。
//
// 编译失败一律走 writePreviewCompileRejected：状态码仍是 422（对作者是业务信息，
// 不是服务端故障），但**错误原文不再进响应** —— 它带节点路径与模板片段，
// 只进结构化日志；对外是三条可归因文案 + 一条归口文案。
// 代价要写在这里：原先透出的「轮播至少需要一个 slide」这类逐组件提示不再出现在
// 画布上（原文仍在日志里），换成了分类文案里说清的「怎么办」。这是按「原文只进
// 日志」的纪律收的，若将来要恢复逐组件提示，正确做法是让验证器返回**结构化**的
// 问题列表（组件 + 槽位 + 处置），而不是把 err.Error() 拼回响应。
func (h *Handle) renderPreview(c *gin.Context, document json.RawMessage, projectID, currentPath string,
	withEditorBridge bool, kind workbenchservice.PreviewDocKind) {
	// 预览语言：?lang= 显式指定（工作台多语言预览切换），空 = 站点默认语言。
	// 画布标记层与画布联动脚本同开关：两者都只对编辑器有意义，普通预览（编辑器外链预览）
	// 不该多出那一层 div —— 它会让「预览 HTML」与产物 HTML 不再是同一份结构。
	//
	// 编译与分级全在 service.RenderPreview（html + outcome + err 三返回值）；
	// 本函数只把 outcome 翻成状态码与响应体，分类判据（哪种失败给哪条文案）在 service 里。
	html, outcome, err := h.svc.RenderPreview(c.Request.Context(), document, projectID, currentPath,
		c.Query("lang"), withEditorBridge)
	switch outcome {
	case workbenchservice.PreviewOK:
		if withEditorBridge {
			html = []byte(injectEditorBridge(string(html), i18n.TranslateFunc(response.RequestLanguage(c))))
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", html)
	case workbenchservice.PreviewInvalidDocument:
		c.String(http.StatusBadRequest, workbenchShortText(c, workbenchenums.ErrDraftDecodeFailed))
	case workbenchservice.PreviewCompileRejected:
		writePreviewCompileRejected(c, document, kind, err)
	default:
		// 这里原先直写 workbenchenums.MsgInternalError —— 那是一个裸 key，
		// 画布响应体不经过 pkg/response 的翻译层，浏览器里看到的就是 "MsgInternalError"。
		c.String(http.StatusInternalServerError, adminui.PageInternalText(c))
	}
}

// editorBridgeJSSource 画布 iframe 内的编辑器桥接脚本（源码见 scripts/editor_bridge.js）。
//
// 从 387 行的 Go 字符串常量挪出来的理由：常量没有语法高亮、没有 lint、无法被任何
// 语法检查覆盖（此前只有 4 处 strings.Contains 守着几个字面量，脚本里一个括号写错
// 要等画布真的坏掉才知道）。独立 .js 之后 `node --check` 与行为测试都能跑。
//
//go:embed scripts/editor_bridge.js
var editorBridgeJSSource string

// editorBridgeScriptFor 按当前语言产出桥接脚本：占位符换成译文后返回完整 <script>。
func editorBridgeScriptFor(tr func(key, fallback string) string) string {
	pairs := make([]string, 0, len(editorBridgeTexts)*2)
	for _, it := range editorBridgeTexts {
		pairs = append(pairs, "{{"+it.Name+"}}", jsSingleQuoted(tr(it.Key, it.Fallback)))
	}
	body := strings.NewReplacer(pairs...).Replace(editorBridgeJSSource)
	// 不在这里拼 <script> 标签：Go 侧不产出 HTML 结构（html_boundary_test.go 的预算
	// 登记的就是这件事）。标签由调用方的模板 / 注入点提供。
	return body
}

// editorBridgeTexts 桥接脚本里的可见文案：占位符名 → 词条 key + 中文兜底。
//
// 脚本以 `{{bridge.<name>}}` 书写、注入时替换，而不是把中文直接写在脚本里 ——
// 这些串会由 iframe 内的 JS 输出（浮标文字、右键菜单项、快捷条 title），
// 硬编码中文在英文画布上就是漏译。占位符形态还让「脚本里的占位符集合 =
// 本表的名字集合」成为可机器校验的判据（见 editor_bridge_test.go），
// 漏登记一个的表现是按钮上原样显示花括号，而这类缺陷不会让任何断言变红。
var editorBridgeTexts = []struct{ Name, Key, Fallback string }{
	{"bridge.insert", workbenchenums.BridgeInsert, "插入组件"},
	{"bridge.editText", workbenchenums.BridgeEditText, "编辑文本"},
	{"bridge.copy", workbenchenums.BridgeCopy, "复制"},
	{"bridge.cut", workbenchenums.BridgeCut, "剪切"},
	{"bridge.pasteInside", workbenchenums.BridgePasteInside, "粘贴到内部"},
	{"bridge.moveUp", workbenchenums.BridgeMoveUp, "上移"},
	{"bridge.moveDown", workbenchenums.BridgeMoveDown, "下移"},
	{"bridge.delete", workbenchenums.BridgeDelete, "删除"},
	{"bridge.entranceGroup", workbenchenums.BridgeEntranceGroup, "入场动画"},
	{"bridge.hoverLift", workbenchenums.BridgeHoverLift, "悬浮上浮"},
}

// jsSingleQuoted 把译文转义成能放进单引号 JS 字符串字面量的形态。
//
// 词条是运营可改的数据，不是编译期常量：一个撇号（如 "Don't"）就会当场把脚本打断，
// 而断掉的后果是**画布里的桥接整体失效**（选中、拖放、右键全部无反应）——
// 报错在 iframe 控制台，父窗口看起来只是「点了没反应」。
func jsSingleQuoted(s string) string {
	return strings.NewReplacer(
		`\`, `\\`,
		`'`, `\'`,
		"\r", `\r`,
		"\n", `\n`,
		"</", `<\/`,
	).Replace(s)
}

// editorBridgeScriptTag 把脚本体包成 <script> 元素。
//
// 唯一的 HTML 结构构造点，集中在此便于门禁记账（html_boundary_test.go 的预算）。
// 从前它散在 editorBridgeScriptFor 里，与「Go 不拼 HTML」的分层规则冲突。
func editorBridgeScriptTag(body string) string {
	return "<script>" + body + "</script>"
}

// injectEditorBridge 把编辑器桥接脚本（按请求语言取词后）追加到 </body> 前。
func injectEditorBridge(html string, tr func(key, fallback string) string) string {
	idx := strings.LastIndex(html, "</body>")
	if idx < 0 {
		return html
	}
	// <script> 包装由模板层提供（scripts/editor_bridge.html），Go 只负责取词后的脚本体。
	// 这样「HTML 结构住在模板里」这条分层规则对注入路径也成立。
	return html[:idx] + editorBridgeScriptTag(editorBridgeScriptFor(tr)) + html[idx:]
}

// workbench_page.go — 工作台编辑目标描述符（审计 EDT-017）。
//
// 工作台此前靠 meta.saveBase 字符串分派（page / block / template 各写一段 if）：
// 接入一种新文档类型要在保存、预览、校验、历史四处各加一个分支，而这些分派散在
// 前端 JS 里 —— 漏改一处不会编译失败，只会在用户点保存时表现为「什么都没发生」。
//
// 描述符把「这个目标有哪些端点、支持哪些动作、保存体长什么样」变成**数据**：
// 前端只按描述符走，新增目标类型 = 在这里注册一条，前端零改动。

// 编辑目标类型。
const (
	EditTargetPage     = "page"
	EditTargetBlock    = "block"
	EditTargetTemplate = "template"
	// EditTargetInstance 实例编辑模式（迁移 281，AGENTS.md）：
	// 画布改的是该实例自己的覆盖文档，保存只影响本实例。
	EditTargetInstance = "instance"
)

// TargetEndpoint 目标的一个端点（method 省略即 POST：项目的写操作一律 POST）。
type TargetEndpoint struct {
	Path string `json:"path"`
}

// TargetSaveBody 保存请求体的构造方式。
//
// 三种目标的键名各不相同（page 用 draftDocument + expectedVersion + draftPath，
// block 用 document 且额外带 name，template 用 draftDocument 无版本），
// 键名放数据里，前端就不必认识任何一种目标。
type TargetSaveBody struct {
	// IDKey 目标 id 的键名。
	IDKey string `json:"idKey"`
	// DocumentKey 文档的键名。
	DocumentKey string `json:"documentKey"`
	// VersionKey 乐观锁版本键（空 = 该目标不用版本）。
	VersionKey string `json:"versionKey,omitempty"`
	// PathKey 路径键（空 = 该目标没有 URL 概念）。
	PathKey string `json:"pathKey,omitempty"`
	// Extras 附加字段：请求体键 → meta 里的来源键（如 block 的 name ← blockName）。
	Extras map[string]string `json:"extras,omitempty"`
}

// TargetCapabilities 能力开关：前端据此显示或隐藏按钮。
//
// 没有这层的话，「块没有发布链」这种事只能靠前端记：按钮照常显示，点了报错，
// 或者更糟 —— 点了之后静默失败。
type TargetCapabilities struct {
	Publish  bool `json:"publish"`
	Build    bool `json:"build"`
	Rollback bool `json:"rollback"`
	History  bool `json:"history"`
	URL      bool `json:"url"`
}

// EditTarget 一种可编辑文档类型的完整描述。
type EditTarget struct {
	Type     string             `json:"type"`
	Save     TargetEndpoint     `json:"save"`
	SaveBody TargetSaveBody     `json:"saveBody"`
	Caps     TargetCapabilities `json:"caps"`
}

// workbenchTargets 目标注册表 —— **新增编辑目标只需要在这里加一条**。
var workbenchTargets = map[string]EditTarget{
	EditTargetPage: {
		Type: EditTargetPage,
		Save: TargetEndpoint{Path: "/api/page/draft/save"},
		SaveBody: TargetSaveBody{
			IDKey: "id", DocumentKey: "draftDocument",
			VersionKey: "expectedVersion", PathKey: "draftPath",
		},
		// 手工页面是唯一有完整发布链的目标（构建 / 发布 / 回滚 / 历史 / 改 URL）。
		Caps: TargetCapabilities{Publish: true, Build: true, Rollback: true, History: true, URL: true},
	},
	EditTargetBlock: {
		Type: EditTargetBlock,
		Save: TargetEndpoint{Path: "/admin/blocks/save-content"},
		SaveBody: TargetSaveBody{
			IDKey: "id", DocumentKey: "document",
			// returnUrl：菜单页「新建面板块并编辑」带过来的回跳目标，随保存请求体回传。
			// 白名单校验两处都做（这里只做搬运，消费侧 SaveBlockContent 再校验一次）——
			// 前端 meta 是可被改写的中间态，不能当成「已经校验过了」。
			Extras: map[string]string{"name": "blockName", "returnUrl": "returnUrl"},
		},
		// 全局块没有 URL、没有独立产物：它是被引用展开进别人的产物的。
		Caps: TargetCapabilities{},
	},
	EditTargetTemplate: {
		Type:     EditTargetTemplate,
		Save:     TargetEndpoint{Path: "/api/contenttemplate/update"},
		SaveBody: TargetSaveBody{IDKey: "id", DocumentKey: "draftDocument"},
		// 模板保存即产生新版本（无独立发布动作），也没有自己的 URL。
		Caps: TargetCapabilities{},
	},
	// 实例编辑模式（迁移 281，AGENTS.md）：保存只影响本实例
	//（覆盖文档 + 重编译发布），不产生模板新版本，也没有自己的 URL。
	EditTargetInstance: {
		Type: EditTargetInstance,
		Save: TargetEndpoint{Path: "/workbench/instance/save"},
		SaveBody: TargetSaveBody{
			IDKey: "id", DocumentKey: "draftDocument",
			Extras: map[string]string{"projectId": "projectId"},
		},
		Caps: TargetCapabilities{},
	},
}

// workbenchTargetOf 取目标描述符；类型未注册时 panic。
//
// panic 而不是返回空描述符：三个注入点写的都是常量类型，未注册只可能是
// 「加了新目标常量却忘了在注册表里加一条」—— 这种错误要在首次渲染就炸出来，
// 而不是把一个空描述符发给前端（前端会静默退回默认行为，问题看不见）。
func workbenchTargetOf(targetType string) EditTarget {
	target, ok := EditTargetFor(targetType)
	if !ok {
		panic("工作台编辑目标未注册: " + targetType)
	}
	return target
}

// EditTargetFor 按类型取描述符；未知类型返回 false（调用方据此明确报错，不静默降级）。
func EditTargetFor(targetType string) (EditTarget, bool) {
	t, ok := workbenchTargets[targetType]
	return t, ok
}

// WorkbenchTargets 返回全部目标的确定性列表（测试与前端契约共用）。
func WorkbenchTargets() []EditTarget {
	types := make([]string, 0, len(workbenchTargets))
	for t := range workbenchTargets {
		types = append(types, t)
	}
	sort.Strings(types)
	out := make([]EditTarget, 0, len(types))
	for _, t := range types {
		out = append(out, workbenchTargets[t])
	}
	return out
}
