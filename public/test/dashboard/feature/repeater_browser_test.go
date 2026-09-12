package feature

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"go_wp/internal/builder"
	"go_wp/internal/middleware/builtin"
	"go_wp/internal/templates"

	"github.com/gin-gonic/gin"
)

// 手工浏览器夹具，复用真实 Inspector handler/模板/JS/撤销逻辑/编译器，无数据库。
// GOWP_REPEATER_BROWSER=1 go test ./public/test/dashboard/feature -run TestRepeaterBrowserFixture -v -timeout 20m
// 只监听回环地址；不是生产应用，也不提供持久化写入口。
func TestRepeaterBrowserFixture(t *testing.T) {
	if os.Getenv("GOWP_REPEATER_BROWSER") != "1" {
		t.Skip("浏览器夹具按需启用，见文件头命令")
	}
	router := newInspectorRouter(t)
	router.Use(builtin.StaticCacheMiddleware())
	router.Static("/static", "../../../../internal/templates/static")
	// 与历史页面缓存隔离，验证时始终从本次工作区读取实际资产。
	router.Static("/qa-static", "../../../../internal/templates/static")
	fixture, err := os.ReadFile("../fixtures/repeater_browser.html")
	if err != nil {
		t.Fatal(err)
	}
	assets := httptest.NewRecorder()
	if err := router.HTMLRender.Instance("partials/ui_scripts.html", nil).Render(assets); err != nil {
		t.Fatal(err)
	}
	router.GET("/", func(c *gin.Context) {
		// 夹具升级前曾不带缓存头；版本参数避免浏览器复用旧控件脚本。
		uiScripts := strings.ReplaceAll(assets.Body.String(), `/static/`, `/qa-static/`)
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(strings.ReplaceAll(string(fixture), "<!-- UI_SCRIPTS -->", uiScripts)))
	})
	router.StaticFile("/fixture.mjs", "../fixtures/repeater_browser.mjs")
	set, err := templates.NewComponentSet("../../../../internal/templates/components")
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
	listener, err := net.Listen("tcp", "127.0.0.1:19127")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: router, ReadHeaderTimeout: 3 * time.Second}
	defer server.Close()
	go server.Serve(listener)
	t.Log("浏览器验证地址：http://127.0.0.1:19127（15 分钟后自动退出）")
	<-time.After(15 * time.Minute)
}
