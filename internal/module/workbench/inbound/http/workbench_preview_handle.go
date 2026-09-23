package workbenchhttp

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	blockcontract "go_wp/internal/module/block/contract"
	pagecontract "go_wp/internal/module/page/contract"
	plugincontract "go_wp/internal/module/plugin/contract"
	workbenchenums "go_wp/internal/module/workbench/enums"

	"github.com/gin-gonic/gin"
)

// workbench_preview_handle.go - 工作台预览渲染（页面 / 块草稿编译直出）。

// Preview 基于已保存草稿轻量编译并在独立响应中输出完整 HTML 文档
// （0-A1 §4.2 隔离预览：不落盘、不影响线上产物）。
func (h *Handle) Preview(c *gin.Context) {
	pageID := strings.TrimSpace(c.Query("id"))
	if pageID == "" {
		c.String(http.StatusBadRequest, "缺少页面 id")
		return
	}
	page, err := h.pageOf(c, pageID)
	if err != nil {
		c.String(http.StatusNotFound, "页面不存在")
		return
	}
	h.renderPreview(c, page.DraftDocument, page.ProjectID, page.DraftPath, c.Query("editor") == "1", previewDocPage)
}

// BlockPreview 全局块画布预览（工作台块编辑模式 iframe 内嵌）。
func (h *Handle) BlockPreview(c *gin.Context) {
	blockID := strings.TrimSpace(c.Query("id"))
	if blockID == "" {
		c.String(http.StatusBadRequest, "缺少块 id")
		return
	}
	block, err := h.blocks.Detail(c.Request.Context(), &blockcontract.DetailReq{ID: blockID})
	if err != nil || block == nil {
		c.String(http.StatusNotFound, "全局块不存在")
		return
	}
	h.renderPreview(c, block.Document, block.ProjectID, "", c.Query("editor") == "1", previewDocBlock)
}

// PreviewDraft 基于未保存 AST 返回临时预览，不持久化、不写 Artifact、不影响发布指针。
func (h *Handle) PreviewDraft(c *gin.Context) {
	pageID := strings.TrimSpace(c.PostForm("id"))
	document := json.RawMessage(c.PostForm("draftDocument"))
	version, err := strconv.ParseInt(c.PostForm("expectedVersion"), 10, 64)
	if pageID == "" || err != nil || len(document) == 0 {
		c.String(http.StatusBadRequest, "草稿文档解析失败")
		return
	}
	page, err := h.pageOf(c, pageID)
	if err != nil {
		c.String(http.StatusNotFound, "页面不存在")
		return
	}
	if version != page.DraftVersion {
		c.String(http.StatusConflict, "草稿版本已更新，请刷新后重试")
		return
	}
	h.renderPreview(c, document, page.ProjectID, page.DraftPath, true, previewDocPage)
}

// renderPreview 只完成 AST 校验与编译，响应生命周期结束即丢弃结果。
// 编译复用 page 模块 CompilePreview（与正式构建同源装配管线，docs/06 §10）：
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
func (h *Handle) renderPreview(c *gin.Context, document json.RawMessage, projectID, currentPath string, withEditorBridge bool, kind previewDocKind) {
	// 预览语言：?lang= 显式指定（工作台多语言预览切换），空 = 站点默认语言。
	// 画布标记层与画布联动脚本同开关：两者都只对编辑器有意义，普通预览（编辑器外链预览）
	// 不该多出那一层 div —— 它会让「预览 HTML」与产物 HTML 不再是同一份结构。
	html, err := h.pages.CompilePreview(c.Request.Context(), document, projectID, currentPath, c.Query("lang"), withEditorBridge)
	if err != nil {
		switch {
		case errors.Is(err, pagecontract.ErrPreviewInvalidDocument):
			c.String(http.StatusBadRequest, "草稿文档解析失败")
		case errors.Is(err, pagecontract.ErrPreviewCompileFailed):
			writePreviewCompileRejected(c, document, kind, err)
		default:
			c.String(http.StatusInternalServerError, workbenchenums.MsgInternalError)
		}
		return
	}
	if withEditorBridge {
		html = []byte(injectEditorBridge(string(html)))
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", html)
}

// pluginAssembly 启用插件装配素材（无插件模块契约或无启用插件时返回 nil）。
// 单请求内缓存（同一请求多次调用只查一次库）。
func (h *Handle) pluginAssembly(c *gin.Context) *plugincontract.Assembly {
	if h.plugins == nil {
		return nil
	}
	if v, ok := c.Get("pluginAssembly"); ok {
		if asm, ok := v.(*plugincontract.Assembly); ok {
			return asm
		}
	}
	asm, err := h.plugins.EnabledAssembly(c.Request.Context())
	if err != nil {
		return nil
	}
	if asm != nil && (len(asm.PluginFS) > 0 || len(asm.Specs) > 0) {
		c.Set("pluginAssembly", asm)
		return asm
	}
	return nil
}

func workbenchTitle(page *pagecontract.PageResp) string {
	if page == nil || strings.TrimSpace(page.ID) == "" {
		return "可视化编辑器"
	}
	var doc struct {
		Settings struct {
			SEO struct {
				Title string `json:"title"`
			} `json:"seo"`
		} `json:"settings"`
	}
	_ = json.Unmarshal(page.DraftDocument, &doc)
	if doc.Settings.SEO.Title != "" {
		return doc.Settings.SEO.Title
	}
	return "编辑器 · " + page.DraftPath
}
