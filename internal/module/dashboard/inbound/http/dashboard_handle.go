// Package dashboardhttp 承载需要后端逻辑的后台页面入口（仪表盘与可视化编辑器）。
//
// 纯静态模板直接放 internal/templates；只有需要后端数据/逻辑的页面才落到本模块。
// 可视化编辑器（Visual Workbench，docs/03-A）外壳在本模块装配：
// 页面壳 + 草稿 AST 注入 + 预览编译直出。
package dashboardhttp

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	dashboardenums "go_wp/internal/module/dashboard/enums"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	projectcontract "go_wp/internal/module/project/contract"

	"go_wp/internal/builder"

	"github.com/gin-gonic/gin"
)

// Handle 页面处理器，聚合 dashboard 相关 handler。
type Handle struct {
	pages    pagecontract.PageService
	projects projectcontract.ProjectService
}

// NewHandle 创建页面处理器；pages/projects 为 page 与 project 模块契约。
func NewHandle(pages pagecontract.PageService, projects projectcontract.ProjectService) *Handle {
	return &Handle{pages: pages, projects: projects}
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
	// 组件 Inspector 面板 schema（docs/02-C3）：声明式 Controls 驱动检查器表单，
	// 前端按 content/style/advanced 分组渲染，替代硬编码字段。
	schemas, err := builder.ComponentSchemas()
	if err != nil {
		c.String(http.StatusInternalServerError, "组件 schema 生成失败")
		return
	}
	schemasJSON, err := json.Marshal(schemas)
	if err != nil {
		c.String(http.StatusInternalServerError, "组件 schema 序列化失败")
		return
	}
	c.HTML(http.StatusOK, "workbench/layout", gin.H{
		"title":     workbenchTitle(page),
		"pageId":    page.ID,
		"draftPath": page.DraftPath,
		"version":   page.DraftVersion,
		"document":  string(documentJSON),
		"meta":      string(metaJSON),
		"schemas":   string(schemasJSON),
	})
}

// Preview 基于已保存草稿轻量编译并在独立响应中输出完整 HTML 文档
// （0-A1 §4.2 隔离预览：不落盘、不影响线上产物）。
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
	h.renderPreview(c, page.DraftDocument, c.Query("editor") == "1")
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
	page, err := h.pages.Detail(c.Request.Context(), &pagedto.DetailReq{ID: pageID})
	if err != nil {
		c.String(http.StatusNotFound, "页面不存在")
		return
	}
	if version != page.DraftVersion {
		c.String(http.StatusConflict, "草稿版本已更新，请刷新后重试")
		return
	}
	h.renderPreview(c, document, true)
}

// renderPreview 只完成 AST 校验与编译，响应生命周期结束即丢弃结果。
func (h *Handle) renderPreview(c *gin.Context, document json.RawMessage, withEditorBridge bool) {
	var docPage *builder.Page
	if err := json.Unmarshal(document, &docPage); err != nil || docPage == nil {
		c.String(http.StatusBadRequest, "草稿文档解析失败")
		return
	}
	compiled, err := builder.Compile(docPage)
	if err != nil {
		c.String(http.StatusUnprocessableEntity, "编译失败: %s", err.Error())
		return
	}
	html := builder.RenderDocument(compiled)
	if withEditorBridge {
		html = injectEditorBridge(html)
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
}

// editorBridgeScript 在 iframe 内运行的编辑器桥接脚本（仅编辑器预览注入）。
// 职责：节点选择标记还原、点击选中上报、选中高亮、容器/元素下方
// 「+ 插入组件」浮标、拖放落点指示线样式。
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

  var style = document.createElement('style');
  style.textContent = [
    '[data-wp-id]:hover{outline:1px solid rgba(37,99,235,.45);outline-offset:-1px;cursor:pointer;}',
    '[data-wp-id].wb-selected{outline:2px solid #2563eb;outline-offset:-2px;}',
    '.wb-bridge-insert{',
    '  position:absolute;z-index:99998;left:50%;transform:translateX(-50%);',
    '  padding:4px 12px;font-size:12px;line-height:1.6;white-space:nowrap;',
    '  color:#fff;background:#2563eb;border:none;border-radius:999px;cursor:pointer;',
    '  box-shadow:0 2px 10px rgba(37,99,235,.45);',
    '}',
    '.wb-bridge-insert:hover{background:#1d4ed8;}',
    '[data-wp-id].wb-drop-before{box-shadow:0 -3px 0 0 #2563eb;}',
    '[data-wp-id].wb-drop-after{box-shadow:0 3px 0 0 #2563eb;}',
    '[data-wp-id].wb-drop-inside{outline:2px dashed #2563eb;outline-offset:-2px;}'
  ].join('');
  document.head.appendChild(style);

  document.addEventListener('click', function(ev){
    var target = ev.target.closest('[data-wp-id]');
    if(!target) return;
    ev.preventDefault(); ev.stopPropagation();
    parent.postMessage({type:'wb-select', id: target.getAttribute('data-wp-id')}, location.origin);
  }, true);

  // 「+ 插入组件」浮标：父窗口在选中变化时发 wb-mark-selected，
  // 此处把浮标定位到选中元素底部中央；点击上报插入意图，
  // 由父窗口根据 AST 判断目标是容器(inside)还是普通元素(after)。
  var insertBtn = document.createElement('button');
  insertBtn.type = 'button';
  insertBtn.className = 'wb-bridge-insert';
  insertBtn.textContent = '+ 插入组件';
  insertBtn.style.display = 'none';
  document.body.appendChild(insertBtn);
  insertBtn.addEventListener('click', function(ev){
    ev.preventDefault(); ev.stopPropagation();
    var id = insertBtn.getAttribute('data-target-id') || '';
    if (id) parent.postMessage({type:'wb-insert-here', id: id}, location.origin);
  });

  window.addEventListener('message', function(ev){
    if (ev.origin !== location.origin || !ev.data) return;
    if (ev.data.type === 'wb-mark-selected') {
      var prev = document.querySelector('[data-wp-id].wb-selected');
      if (prev) prev.classList.remove('wb-selected');
      var el = ev.data.id ? document.querySelector('[data-wp-id="' + ev.data.id + '"]') : null;
      if (el) {
        el.classList.add('wb-selected');
        var rect = el.getBoundingClientRect();
        insertBtn.style.display = 'block';
        insertBtn.setAttribute('data-target-id', ev.data.id);
        insertBtn.style.top = (rect.bottom + window.scrollY + 4) + 'px';
      } else {
        insertBtn.style.display = 'none';
      }
    }
  });

  // 拖放落点指示：父窗口 bindCanvasDrop 在 dragover 时给目标加类，
  // 这里只负责样式；drop/dragleave 时父窗口负责移除。
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
