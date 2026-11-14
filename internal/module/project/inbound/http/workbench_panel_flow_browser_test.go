package projecthttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	pageview "go_wp/pkg/view"
)

// 工作台三块面板的**真实前端闭环**：生产片段 + 生产模块，真实 Chrome。
//
// 与 internal/templates/workbench_panel_browser_test.cjs 的分工：
//
//	· 那一份手写面板 HTML，证明共享执行链（CSRF / HTML 校验 / 失败区 / 迟到）；
//	· 这一份把 `/workbench/settings`、`/workbench/global` 的**真实处理器与 Jet 片段**
//	  跑起来，并加载 editor-app/methods/app-panels.js 的**真实方法**，验证三个
//	  断点确实闭环：保存按钮存在、成功事件读 id 属性（不被 name="id" 遮蔽）、
//	  密码哈希回填进文档、主题状态只有一份（再打开提交新值）。
//
// 宿主侧副作用（setNotice / refreshCanvas / snapshot）用最小替身：这些是应用层行为，
// 不是本用例要证明的面板逻辑；面板方法与片段都是真的。
func TestWorkbenchPanelFlowInRealBrowser(t *testing.T) {
	if os.Getenv("GOWP_WORKBENCH_PANEL_BROWSER") != "1" {
		t.Skip("设置 GOWP_WORKBENCH_PANEL_BROWSER=1 运行真实片段 + 真实模块的浏览器闭环")
	}
	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)

	// /static 由真实复合资产源提供：ES module 之间的相对 import 与生产同源。
	engine.GET("/static/*filepath", func(c *gin.Context) {
		name := strings.TrimPrefix(c.Param("filepath"), "/")
		body, err := templates.AssetFile(name)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		contentType := "application/octet-stream"
		switch filepath.Ext(name) {
		case ".js", ".cjs":
			contentType = "text/javascript; charset=utf-8"
		case ".css":
			contentType = "text/css; charset=utf-8"
		}
		c.Data(http.StatusOK, contentType, body)
	})

	// 面板端点与生产注册同形：片段协议 + 渲染出口 + 真实处理器。
	engine.POST("/workbench/settings",
		pageview.HTMLFragment, pageview.Slot(SettingsPanelFragmentTemplate, pageview.RenderBare), workbenchSettingsPanel)
	engine.POST("/workbench/seo-score-panel",
		pageview.HTMLFragment, pageview.Slot(SeoScoreFragmentTemplate, pageview.RenderBare), workbenchSeoScorePanel)
	engine.POST("/workbench/global",
		pageview.HTMLFragment, pageview.Slot(GlobalPanelFragmentTemplate, pageview.RenderBare), workbenchGlobalPanel)
	engine.POST("/admin/themes/settings/save", pageview.HTMLFragment, func(c *gin.Context) {
		// 保存端点在这里只作为「成功回执」：主题落库由后台页面用例覆盖。
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(`<p role="status">主题已保存</p>`))
	})

	engine.GET("/", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(panelFlowShell))
	})

	server := httptest.NewServer(engine)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "--test", "workbench_panel_flow_browser_test.cjs")
	cmd.Env = append(os.Environ(), "GOWP_WORKBENCH_PANEL_BASE_URL="+server.URL)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("真实片段 + 真实模块的浏览器闭环失败：%v\n%s", err, output)
	}
	t.Logf("%s", output)
}

// panelFlowShell 是工作台布局里三块面板宿主的同形外壳。
//
// 它只保留**宿主职责**：表单属性、交换目标与状态行（这些都写在布局里，
// 不属片段）；片段内容与脚本全部来自生产源。
const panelFlowShell = `<!DOCTYPE html><html lang="zh-CN"><head>
<meta charset="UTF-8"><meta name="csrf-token" content="flow-token"></head>
<body>
<section data-write-host id="wb-settings-body">
  <p class="form-hint" data-write-status role="status" hidden></p>
  <form id="wb-settings-form" data-write hx-post="/workbench/settings"
        hx-trigger="wb:settings-panel from:body" hx-target="#wb-settings-content"
        hx-swap="innerMorph" hx-sync="this:replace" data-write-builder="workbench-settings">
    <div id="wb-settings-content"></div>
  </form>
  <form id="wb-settings-password-form" data-write hx-post="/workbench/settings"
        hx-trigger="wb:settings-password from:body" hx-target="#wb-settings-content"
        hx-swap="innerMorph" hx-sync="#wb-settings-content:replace"
        data-write-builder="workbench-settings"></form>
  <form id="wb-seo-form" data-write hx-post="/workbench/seo-score-panel"
        hx-trigger="wb:seo-panel from:body" hx-target="#wb-seo-score"
        hx-swap="innerMorph" hx-sync="this:replace" data-write-builder="workbench-seo"></form>
</section>
<section data-write-host id="wb-global-body">
  <p class="form-hint" data-write-status role="status" hidden></p>
  <form id="wb-global-load" data-write hx-post="/workbench/global"
        hx-trigger="wb:global-panel from:body" hx-target="#wb-global-content"
        hx-swap="innerMorph" hx-sync="this:replace" data-write-builder="workbench-global"></form>
  <div id="wb-global-content"></div>
  <div role="status" aria-live="polite" id="wb-global-save-result"></div>
</section>
<script type="application/json" id="wb-meta">{"themeId":"theme-flow-1","themeSettings":{"colors":{"primary":"#111111","text":"#333333"},"headerBlockId":"blk-header"},"draftPath":"/flow"}</script>
<script type="application/json" id="wb-bootstrap">{"settings":{"access":{"type":"password"},"seo":{"title":"标题"}},"root":[]}</script>
<script type="application/json" id="wb-schemas">{}</script>
<script src="/static/js/ui/htmx.min.js"></script>
<script src="/static/js/ui/write-base.js"></script>
<script src="/static/js/editor-app/panel-requests.js"></script>
<script type="module">
import { workbench } from '/static/js/editor/index.js';
import { meta } from '/static/js/editor/core.js';
import { appPanelMethods } from '/static/js/editor-app/methods/app-panels.js';

// 宿主副作用替身：只记录，不实现应用业务（应用层行为不属本用例）。
const wb = workbench(appPanelMethods);
wb.meta = meta;
wb.notice = '';
wb.saveState = 'saved';
wb.setNotice = function (text) { this.notice = text || ''; };
wb.refreshCanvas = function () { window.__refreshed = (window.__refreshed || 0) + 1; };
wb.snapshot = function () { window.__snapshots = (window.__snapshots || 0) + 1; this.saveState = 'dirty'; };
window.__wb = wb;

// 请求观测：XHR 层记录真实发出/回执，不修改任何请求语义。
window.__saves = [];
window.__loads = [];
const open = XMLHttpRequest.prototype.open;
const send = XMLHttpRequest.prototype.send;
XMLHttpRequest.prototype.open = function (method, url) { this.__url = url; this.__method = method; return open.apply(this, arguments); };
XMLHttpRequest.prototype.send = function (body) {
  const url = String(this.__url || '');
  const entry = { method: this.__method, path: new URL(url, location.href).pathname,
                  contentType: this.getRequestHeader ? '' : '', body: typeof body === 'string' ? body : '' };
  const header = this.setRequestHeader;
  this.__ct = '';
  this.setRequestHeader = function (k, v) { if (String(k).toLowerCase() === 'content-type') this.__ct = v; return header.apply(this, arguments); };
  entry.contentType = this.__ct;
  this.addEventListener('load', function () {
    entry.contentType = this.__ct || entry.contentType;
    try { const parsed = JSON.parse(entry.body); if (entry.path === '/workbench/global') window.__loads.push(parsed); } catch (e) { /* 非 JSON */ }
    if (entry.path === '/admin/themes/settings/save') window.__saves.push(entry);
  });
  return send.apply(this, arguments);
};
htmx.process(document.body);
window.__flowReady = true;
</script>
</body></html>`
