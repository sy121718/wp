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

// 工作台面板的输入矩阵：三视口（1440/768/375）× 四种输入（鼠标/滚轮/触摸/键盘）。
//
// 与 workbench_panel_flow_browser_test.go 的分工：
//
//	· 那一份只跑桌面视口，证明面板契约（保存入口、成功监听、密码回填、状态单源）；
//	· 这一份只跑「输入方式 × 视口」这一维，证明换设备/换输入后同一动作仍落到同一请求。
//
// 滚轮路径此前一直没有有效证据：不是滚轮不能操作，而是没人证明过「滚到目标再操作」
// 在折叠/滚动容器里仍然有效。这里显式先滚一段再滚回，再点按钮。
func TestWorkbenchPanelInputMatrixInRealBrowser(t *testing.T) {
	if os.Getenv("GOWP_WORKBENCH_PANEL_MATRIX") != "1" {
		t.Skip("设置 GOWP_WORKBENCH_PANEL_MATRIX=1 运行三视口 × 四输入矩阵")
	}
	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)

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

	engine.POST("/workbench/settings",
		pageview.HTMLFragment, pageview.Slot(SettingsPanelFragmentTemplate, pageview.RenderBare), workbenchSettingsPanel)
	engine.POST("/workbench/seo-score-panel",
		pageview.HTMLFragment, pageview.Slot(SeoScoreFragmentTemplate, pageview.RenderBare), workbenchSeoScorePanel)
	engine.POST("/workbench/global",
		pageview.HTMLFragment, pageview.Slot(GlobalPanelFragmentTemplate, pageview.RenderBare), workbenchGlobalPanel)
	engine.POST("/admin/themes/settings/save", pageview.HTMLFragment, func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(`<p role="status">主题已保存</p>`))
	})
	engine.GET("/", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(panelFlowShell))
	})
	// 生产布局、CSS 与完整应用入口都不替换；业务持久化另由 PG feature 验证。
	engine.GET("/production", func(c *gin.Context) {
		data := gin.H{
			"title": "主题面板输入矩阵", "csrf_token": "flow-token", "jsVer": "test", "lang": "zh-CN",
			"pageId": "p1", "isBlock": false, "isTemplate": false,
			"previewURL": "/workbench/preview?id=p1&editor=1",
			"document":   `{"settings":{"access":{"type":"password"}},"root":[]}`,
			"meta":       `{"pageId":"p1","themeId":"theme-flow-1","themeSettings":{"colors":{"primary":"#111111","text":"#333333"}},"version":1}`,
			"schemas":    `{}`, "t": func(_ string, fallback string) string { return fallback },
		}
		rec := httptest.NewRecorder()
		if err := engine.HTMLRender.Instance("workbench/layout.html", data).Render(rec); err != nil {
			t.Errorf("生产布局渲染失败：%v", err)
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", rec.Body.Bytes())
	})
	// 画布预览不是本矩阵的验收对象；空文档预览不引入假业务字段。
	engine.POST("/workbench/preview", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(`<!doctype html><html><body></body></html>`))
	})
	engine.POST("/workbench/outline", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(`<ul></ul>`))
	})

	server := httptest.NewServer(engine)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "--test", "workbench_panel_matrix_browser_test.cjs")
	cmd.Env = append(os.Environ(), "GOWP_WORKBENCH_PANEL_BASE_URL="+server.URL)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("面板输入矩阵失败：%v\n%s", err, output)
	}
	t.Logf("%s", output)
}
