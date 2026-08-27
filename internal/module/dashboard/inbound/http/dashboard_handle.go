// Package dashboardhttp 承载需要后端逻辑的后台页面入口（仪表盘与可视化编辑器）。
//
// 纯静态模板直接放 internal/templates；只有需要后端数据/逻辑的页面才落到本模块。
// 可视化编辑器（Visual Workbench，docs/03-A）外壳在本模块装配：
// 页面壳 + 草稿 AST 注入 + 预览编译直出。
package dashboardhttp

import (
	"encoding/json"
	"net/http"
	"strings"

	dashboardenums "go_wp/internal/module/dashboard/enums"
	pagedto "go_wp/internal/module/page/dto"

	"go_wp/internal/builder"

	"github.com/gin-gonic/gin"
)

// Handle 页面处理器，聚合 dashboard 相关 handler。
type Handle struct {
	pages PageReader
}

// NewHandle 创建页面处理器；pages 为 page 模块草稿契约。
func NewHandle(pages PageReader) *Handle {
	return &Handle{pages: pages}
}

// Dashboard 仪表盘页面。
func (h *Handle) Dashboard(c *gin.Context) {
	c.HTML(http.StatusOK, "admin/dashboard", gin.H{
		"title": dashboardenums.MsgDashboardTitle,
		"menu":  "dashboard",
	})
}

// Workbench 可视化编辑器外壳：注入 Page 草稿 AST 与保存接口所需元数据。
func (h *Handle) Workbench(c *gin.Context) {
	pageID := strings.TrimSpace(c.Query("id"))
	if pageID == "" {
		c.String(http.StatusBadRequest, "缺少页面 id")
		return
	}
	page, err := h.pages.Detail(c.Request.Context(), &pagedto.DetailReq{ID: pageID})
	if err != nil {
		c.String(http.StatusNotFound, "页面不存在")
		return
	}
	documentJSON, err := json.Marshal(page.DraftDocument)
	if err != nil {
		c.String(http.StatusInternalServerError, "草稿文档序列化失败")
		return
	}
	metaJSON, err := json.Marshal(gin.H{
		"pageId":    page.ID,
		"draftPath": page.DraftPath,
		"version":   page.DraftVersion,
	})
	if err != nil {
		c.String(http.StatusInternalServerError, "编辑器元数据序列化失败")
		return
	}
	c.HTML(http.StatusOK, "workbench/layout", gin.H{
		"title":     workbenchTitle(page),
		"pageId":    page.ID,
		"draftPath": page.DraftPath,
		"version":   page.DraftVersion,
		"document":  string(documentJSON),
		"meta":      string(metaJSON),
	})
}

// Preview 基于当前草稿轻量编译并在独立响应中输出完整 HTML 文档
// （0-A1 §4.2 隔离预览：不落盘、不影响线上产物）。
//
// 编辑器 iframe 刷新时始终使用数据库草稿；未保存修改只停留在外壳 AST 状态。
// 保存成功后前端刷新 iframe，确保预览和可发布草稿一致。
func (h *Handle) Preview(c *gin.Context) {
	pageID := strings.TrimSpace(c.Query("id"))
	if pageID == "" {
		c.String(http.StatusBadRequest, "缺少页面 id")
		return
	}
	page, err := h.pages.Detail(c.Request.Context(), &pagedto.DetailReq{ID: pageID})
	if err != nil {
		c.String(http.StatusNotFound, "页面不存在")
		return
	}
	document := page.DraftDocument

	var docPage *builder.Page
	if parseErr := json.Unmarshal(document, &docPage); parseErr != nil || docPage == nil {
		c.String(http.StatusBadRequest, "草稿文档解析失败")
		return
	}
	compiled, compileErr := builder.Compile(docPage)
	if compileErr != nil {
		c.String(http.StatusUnprocessableEntity, "编译失败: %s", compileErr.Error())
		return
	}
	html := builder.RenderDocument(compiled)

	// 编辑器 iframe 场景：注入 data-wp-id 标记与选中高亮样式 + 一个极小桥接脚本，
	// 供外壳 workbench.js 实现画布点击选中（规范 §4.1/§5.1）。
	// 注入只发生在内存响应中；产物落盘路径（pipeline Publish）不经过这里，保持纯净。
	if c.Query("editor") == "1" {
		html = injectEditorBridge(html)
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

// editorBridgeScript 在 iframe 内运行的选中联动脚本（仅编辑器预览注入）。
const editorBridgeScript = `<script>
(function(){
  // 编译器把节点 ID 编入 wp-c-* CSS 类；编辑器桥接层将其还原为选择标记。
  document.querySelectorAll('[class]').forEach(function(el){
    el.classList.forEach(function(cls){
      if (cls.indexOf('wp-c-') !== 0) return;
      el.setAttribute('data-wp-id', cls.slice(5));
    });
  });
  document.querySelectorAll('[id]').forEach(function(el){
    if (!el.getAttribute('data-wp-id')) el.setAttribute('data-wp-id', el.id);
  });
  document.addEventListener('click', function(ev){
    var target = ev.target.closest('[data-wp-id]');
    if(!target) return;
    ev.preventDefault(); ev.stopPropagation();
    parent.postMessage({type:'wb-select', id: target.getAttribute('data-wp-id')}, location.origin);
  }, true);
  var style = document.createElement('style');
  style.textContent = '[data-wp-id]:hover{outline:1px solid rgba(37,99,235,.45);outline-offset:-1px;cursor:pointer;}';
  document.head.appendChild(style);
})();
</script>`

// injectEditorBridge 把编辑器桥接脚本追加到 </body> 前。
func injectEditorBridge(html string) string {
	idx := strings.LastIndex(html, "</body>")
	if idx < 0 {
		return html
	}
	return html[:idx] + editorBridgeScript + html[idx:]
}

func workbenchTitle(page *pagedto.PageResp) string {
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
