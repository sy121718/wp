package dashboardhttp

// inspector_panel_bench_test.go — 检查器面板服务端渲染路径的延迟测量（EDT-009）。
//
// 背景：检查器控件渲染曾存在「服务端 HTMX 片段 vs 客户端 JS 拼 DOM」双轨，
// 现渲染层已统一为服务端片段（methods/inspector.js 的 syncInspector 是唯一入口）。
// 本基准用真实 Jet 渲染器测一次 POST /workbench/inspector 片段渲染的完整开销
// （document JSON 解析 → schema 分组 → 模板渲染），作为统一路线的性能证据。
// 选用 core.heading（无 entityref 字段），不触碰数据库，可离线运行。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/templates"
)

const inspectorBenchDoc = `{"root":[{"id":"n1","type":"core.heading","props":{"title":"基准标题","level":"2"},"children":[]}]}`

// postInspector 按客户端真实提交形状（fetchInspectorPanel）发一次请求并校验响应。
func postInspector(b *testing.B, engine *gin.Engine, tab string) {
	b.Helper()
	form := url.Values{}
	form.Set("nodeId", "n1")
	form.Set("document", inspectorBenchDoc)
	form.Set("tab", tab)
	req := httptest.NewRequest(http.MethodPost, "/workbench/inspector", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		b.Fatalf("片段渲染失败，状态 %d: %s", rec.Code, rec.Body.String())
	}
}

// benchInspectorEngine 构造带真实 Jet 渲染器的最小 engine（与 render 测试同口径）。
func benchInspectorEngine(b *testing.B) *gin.Engine {
	b.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.POST("/workbench/inspector", (&Handle{}).InspectorPanel)
	return engine
}

// BenchmarkInspectorPanelHeading 选中一个 core.heading 节点（内容页签完整渲染）。
func BenchmarkInspectorPanelHeading(b *testing.B) {
	engine := benchInspectorEngine(b)
	postInspector(b, engine, "content") // 预热 + 首次校验响应内容
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		postInspector(b, engine, "content")
	}
}

// BenchmarkInspectorPanelHeadingStyle 样式页签（分组最多的路径）。
func BenchmarkInspectorPanelHeadingStyle(b *testing.B) {
	engine := benchInspectorEngine(b)
	postInspector(b, engine, "style")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		postInspector(b, engine, "style")
	}
}

// BenchmarkInspectorPanelEmpty 选中为空（面板占位）的基线。
func BenchmarkInspectorPanelEmpty(b *testing.B) {
	engine := benchInspectorEngine(b)
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest(http.MethodPost, "/workbench/inspector", strings.NewReader("nodeId=&document="))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		b.Fatalf("状态 %d", rec.Code)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/workbench/inspector", strings.NewReader("nodeId=&document="))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
	}
}
