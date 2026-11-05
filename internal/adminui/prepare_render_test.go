package adminui

import (
	"github.com/CloudyKit/jet/v6"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/adminctx"
	admindto "go_wp/internal/module/admin/dto"
	"go_wp/internal/templates"
)

func prepareRenderNavTree() []admindto.MenuTreeNode {
	return []admindto.MenuTreeNode{
		{ID: 1, Title: "商品", Type: 1, Children: []admindto.MenuTreeNode{
			{ID: 2, Title: "商品列表", Path: "/admin/products", Type: 2},
		}},
		{ID: 3, Title: "仪表盘", Path: "/admin", Type: 2},
		{ID: 4, Title: "隐藏菜单项", Path: "/admin/hidden", Type: 2, IsHidden: 1},
	}
}

func TestPrepareInjectsShellFactsWithRealPipeline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	loader := jet.NewInMemLoader()
	loader.Set("probe.html", `{{ .["NavItems"] }}|{{ .["HasSubnav"] }}|{{ .["SidebarPinned"] }}|{{ .["csrf_token"] }}`)
	set := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{"", ".html"}))
	engine.HTMLRender = templates.NewJetHTMLRenderWithSet(set)
	engine.GET("/admin/products", func(c *gin.Context) {

		c.Set(adminctx.PermSetKey, map[string]bool{"product.list": true})
		c.Set(adminctx.ButtonsKey, map[string]bool{"product.create": true})
		c.Set(adminctx.NavTreeKey, prepareRenderNavTree())
		c.Header("Cache-Control", "no-store")
		c.HTML(http.StatusOK, "probe.html", PrepareView(c, gin.H{"Title": "商品"}))
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/products", nil)

	req.AddCookie(&http.Cookie{Name: sidebarCookiePinned, Value: "1"})
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("渲染应成功，实际 %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("页面必须 no-store，实际 %q", got)
	}

	body := rec.Body.String()
	if strings.Contains(body, "<nil>") {
		t.Fatalf("Prepare 注入的壳键缺失：%s", body)
	}
	// 分隔符切出四段：NavGroups、HasSubnav、SidebarPinned、csrf_token。
	parts := strings.Split(body, "|")
	if len(parts) != 4 {
		t.Fatalf("壳键输出形态异常：%q", body)
	}
	if !strings.Contains(parts[0], "商品列表") {
		t.Fatalf("NavItems 应包含授权菜单树：%q", parts[0])
	}
	if strings.TrimSpace(parts[2]) != "true" {
		t.Fatalf("SidebarPinned 应来自 cookie：%q", parts[2])
	}
}
