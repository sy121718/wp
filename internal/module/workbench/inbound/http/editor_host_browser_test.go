package workbenchhttp

import (
	"fmt"
	"net"
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
	json "go_wp/pkg/jsonx"
)

// TestEditorHostBrowserFixture 启动一个仅由 embed 资产与宿主桩组成的编辑器页面，
// 用真实 Chrome 覆盖 page / block / template / instance 四种宿主 × 三视口，
// 验证选择/联动/属性/撤销重做/预览禁写/保存冲突/块回跳/发布失败。
//
// 生产应用鉴权环境不可得时，这是「编辑器工具 + 应用宿主适配」的行为验收；
// 不冒充真实后台登录链路的验收。
func TestEditorHostBrowserFixture(t *testing.T) {
	if os.Getenv("GOWP_EDITOR_BROWSER") != "1" {
		t.Skip("浏览器夹具按需启用：GOWP_EDITOR_BROWSER=1 go test ./internal/module/workbench/inbound/http -run TestEditorHostBrowserFixture")
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	renderer := templates.NewJetHTMLRender("../../../../templates", true)
	router.HTMLRender = renderer
	staticFS, err := templates.EmbeddedStaticFS()
	if err != nil {
		t.Fatal(err)
	}
	router.StaticFS("/static", staticFS)

	// 宿主桩：保存第一次冲突、第二次成功；构建失败；发布成功。
	// 每个视口先调 /qa/reset 开始新一代；每代第一次保存返回 409，重试成功。
	generation := 0
	router.POST("/qa/reset", func(c *gin.Context) {
		generation++
		c.Status(http.StatusNoContent)
	})
	conflicted := map[string]bool{}
	save := func(c *gin.Context) {
		raw, _ := c.GetRawData()
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		version, _ := body["expectedVersion"].(float64)
		key := fmt.Sprintf("%d|%v|%v", generation, version, body["draftDocument"])
		if version < 2 && !conflicted[key] {
			conflicted[key] = true
			c.JSON(http.StatusConflict, gin.H{"code": 409, "message": "版本冲突：请先核对修订历史"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{"draftVersion": 2}})
	}
	for _, path := range []string{
		"/api/page/draft/save", "/admin/blocks/save-content",
		"/api/contenttemplate/update", "/workbench/instance/save",
	} {
		router.POST(path, save)
	}
	router.POST("/api/page/build", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "构建失败：请重试"})
	})
	router.POST("/api/page/publish", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": gin.H{}})
	})
	router.POST("/workbench/inspector", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, `<details class="wb-inspector-group" data-wb-section="content" open><summary class="wb-inspector-section">内容</summary><div class="wb-field"><label>文本</label><input type="text" data-wb-path="text" data-wb-kind="text" value="你好"></div></details>`)
	})
	router.POST("/workbench/outline", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, `<ul><li><div class="wb-node is-selected" role="treeitem" tabindex="0" data-id="n1" data-type="core.text" draggable="true"><button class="wb-caret"></button><span class="wb-node-name" data-named="1">标题文本</span><span class="wb-node-actions"><button type="button" class="wb-node-action" data-wb-op="del">✕</button></span></div></li></ul>`)
	})
	preview := func(c *gin.Context) {
		// 回显本次提交的文档，夹具据此区分「初始加载」与「刷新后重新渲染」。
		submitted := c.PostForm("draftDocument")
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, `<!doctype html><html lang="zh-CN"><body><div data-sky-id="n1" class="sky-c-n1" style="height:1600px">预览正文</div><pre id="preview-doc">%s</pre></body></html>`, submitted)
	}
	for _, path := range []string{"/workbench/preview", "/workbench/block/preview", "/workbench/template/preview"} {
		router.GET(path, preview)
		router.POST(path, preview)
	}
	// 块保存成功后的安全回跳目标。
	router.GET("/admin/blocks", func(c *gin.Context) {
		c.String(http.StatusOK, "<!doctype html><title>块列表</title>")
	})

	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"n1","type":"core.text","props":{"text":"你好"}}]}`
	type mode struct {
		name, preview, meta string
		data                map[string]any
	}
	modes := []mode{
		{
			name: "page", preview: "/workbench/preview?id=p1&editor=1",
			meta: `{"pageId":"p1","projectId":"proj","draftPath":"/demo","version":1,"target":{"type":"page","save":{"path":"/api/page/draft/save"},"saveBody":{"versionKey":"expectedVersion"}}}`,
			data: map[string]any{"isBlock": false, "isTemplate": false},
		},
		{
			name: "block", preview: "/workbench/block/preview?id=b1&editor=1",
			meta: `{"pageId":"b1","projectId":"proj","saveBase":"block","blockName":"页脚块","version":1,"returnUrl":"/admin/blocks","target":{"type":"block","save":{"path":"/admin/blocks/save-content"},"saveBody":{"idKey":"id","documentKey":"document","versionKey":"expectedVersion","extras":{"name":"blockName"}}}}`,
			data: map[string]any{"isBlock": true, "isTemplate": false},
		},
		{
			name: "template", preview: "/workbench/template/preview?id=t1&entityType=product&entityId=e1&editor=1",
			meta: `{"pageId":"t1","projectId":"proj","saveBase":"template","entityType":"product","entityId":"e1","version":1,"target":{"type":"template","save":{"path":"/api/contenttemplate/update"},"saveBody":{"idKey":"id","documentKey":"draftDocument"}}}`,
			data: map[string]any{"isBlock": false, "isTemplate": true, "isStructureTemplate": false, "previewQS": "id=t1&entityType=product&entityId=e1"},
		},
		{
			name: "instance", preview: "/workbench/template/preview?instance=i1&entityType=product&entityId=e1&editor=1",
			meta: `{"pageId":"i1","projectId":"proj","saveBase":"instance","entityType":"product","entityId":"e1","version":1,"instanceMode":true,"renderMode":"template","target":{"type":"instance","save":{"path":"/workbench/instance/save"},"saveBody":{"idKey":"id","documentKey":"draftDocument","extras":{"projectId":"projectId"}}}}`,
			data: map[string]any{"isBlock": false, "isTemplate": true, "instanceMode": true, "renderModeLabel": "跟随模板", "previewQS": "instance=i1&entityType=product&entityId=e1"},
		},
	}

	renderPage := func(m mode) gin.HandlerFunc {
		return func(c *gin.Context) {
			data := map[string]any{
				"title": "浏览器夹具", "csrf_token": "csrf-test", "jsVer": "test", "lang": "zh-CN",
				"pageId": "p1", "previewURL": m.preview,
				"document": doc, "meta": m.meta, "schemas": `{}`,
				"t": func(key, fallback string) string { return fallback },
			}
			for k, v := range m.data {
				data[k] = v
			}
			recorder := httptest.NewRecorder()
			if err := renderer.Instance("workbench/layout.html", data).Render(recorder); err != nil {
				c.String(http.StatusInternalServerError, "render: %v", err)
				return
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", recorder.Body.Bytes())
		}
	}
	for _, m := range modes {
		router.GET("/"+m.name, renderPage(m))
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: router, ReadHeaderTimeout: 3 * time.Second}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	origin := "http://" + listener.Addr().String()

	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("浏览器夹具需要 node")
	}
	script, err := filepath.Abs("../fixtures/editor_host_browser.cjs")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(nodeBin, script)
	cmd.Env = append(os.Environ(), "GOWP_EDITOR_BASE_URL="+origin)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("浏览器夹具失败: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "EDITOR-BROWSER-OK") {
		t.Fatalf("浏览器夹具未报告成功:\n%s", out)
	}
	t.Log(string(out))
}
