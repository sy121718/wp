package workbenchhttp

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"go_wp/internal/middleware/builtin"
	"go_wp/internal/templates"
	"go_wp/pkg/builder"

	"github.com/gin-gonic/gin"
)

func mustStaticFS(t *testing.T) http.FileSystem {
	t.Helper()
	fsys, err := templates.EmbeddedStaticFS()
	if err != nil {
		t.Fatal(err)
	}
	return fsys
}

func TestRepeaterBrowserFixture(t *testing.T) {
	if os.Getenv("GOWP_REPEATER_BROWSER") != "1" {
		t.Skip("浏览器夹具按需启用，见文件头命令")
	}
	router := newInspectorRouter(t)
	router.Use(builtin.StaticCacheMiddleware())
	router.Static("/static", "../../../../internal/templates/static")

	router.StaticFS("/qa-static", mustStaticFS(t))
	fixture, err := os.ReadFile("../fixtures/repeater_browser.html")
	if err != nil {
		t.Fatal(err)
	}
	assets := httptest.NewRecorder()
	if err := router.HTMLRender.Instance("partials/ui_scripts.html", nil).Render(assets); err != nil {
		t.Fatal(err)
	}
	router.GET("/", func(c *gin.Context) {

		uiScripts := strings.ReplaceAll(assets.Body.String(), `/static/`, `/qa-static/`)
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(strings.ReplaceAll(string(fixture), "<!-- UI_SCRIPTS -->", uiScripts)))
	})
	router.StaticFile("/fixture.mjs", "../fixtures/repeater_browser.mjs")
	set, err := templates.NewComponentSet("../../../../templates/components")
	if err != nil {
		t.Fatal(err)
	}
	router.POST("/qa/compile", func(c *gin.Context) {
		data, err := c.GetRawData()
		if err != nil {
			c.String(http.StatusBadRequest, "%v", err)
			return
		}
		page, err := builder.ParsePage(data)
		if err != nil {
			c.String(http.StatusBadRequest, "%v", err)
			return
		}
		compiled, err := builder.Compile(page, builder.WithComponentSet(set))
		if err != nil {
			c.String(http.StatusBadRequest, "%v", err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"html": compiled.HTML})
	})
	// 这是手工验证夹具，不是一次跑完的验收用例：起站点、常驻到 duration。
	// 不设 duration 时默认 15 分钟，短时冒烟用 GOWP_REPEATER_BROWSER_DURATION=30s。
	duration := 15 * time.Minute
	if value := os.Getenv("GOWP_REPEATER_BROWSER_DURATION"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed <= 0 {
			t.Fatal("浏览器夹具持续时间必须为正时长")
		}
		duration = parsed
	}
	listener, err := net.Listen("tcp", "127.0.0.1:19127")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: router, ReadHeaderTimeout: 3 * time.Second}
	defer server.Close()

	// 常驻前先自检一次：页面、注入的 UI 脚本与夹具模块必须都能取到，
	// 否则浏览器打开的是一个坏站点，事后排查容易误判成前端组件问题。
	page := httptest.NewRecorder()
	router.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	if page.Code != http.StatusOK {
		t.Fatalf("夹具页面自检失败：GET / = %d", page.Code)
	}
	if !strings.Contains(page.Body.String(), "/qa-static/") {
		t.Fatal("夹具页面没有注入 UI 脚本（<!-- UI_SCRIPTS --> 未被替换）")
	}
	module := httptest.NewRecorder()
	router.ServeHTTP(module, httptest.NewRequest(http.MethodGet, "/fixture.mjs", nil))
	if module.Code != http.StatusOK || module.Body.Len() == 0 {
		t.Fatalf("夹具模块自检失败：GET /fixture.mjs = %d", module.Code)
	}

	go server.Serve(listener)
	t.Logf("浏览器验证地址：http://%s（%s 后自动退出）", listener.Addr(), duration)
	<-time.After(duration)
}
