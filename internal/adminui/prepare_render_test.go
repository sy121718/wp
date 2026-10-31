package adminui

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

func TestJumpTemplateRendersFullShellWithRealPrepare(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "templates"), true)
	engine.GET("/admin/products", func(c *gin.Context) {

		c.Set(adminctx.PermSetKey, map[string]bool{"product.list": true})
		c.Set(adminctx.ButtonsKey, map[string]bool{"product.create": true})
		c.Set(adminctx.NavTreeKey, prepareRenderNavTree())
		RenderJumpPageWith(c, http.StatusOK, JumpView{OK: true, Msg: "已完成", Back: "/admin", BackText: "返回后台"}, Prepare)
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/products", nil)

	req.AddCookie(&http.Cookie{Name: sidebarCookiePinned, Value: "1"})
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("模板应渲染成功，实际 %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("提示模板页必须 no-store，实际 %q", got)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatalf("提示模板页不是完整页面（缺 </html>）—— 缺键会让 Jet 在中间中断，长度 %d", len(body))
	}
	if strings.Contains(body, "{{") {
		t.Fatal("提示模板页残留模板语法（Jet 在中间某行中断）")
	}

	for _, want := range []string{
		`class="rail" id="adminRail"`,
		`data-group="1"`,
		`<nav class="subnav" id="subnav">`,
		`<a class="rail-btn" href="/admin" title="仪表盘">`,
		`<html lang="`,
		`<meta name="csrf-token"`,
		`X-CSRF-Token`,
		`name="redirect"`,
		`data-jump-state="ok"`,
		`class="jump-msg">已完成</p>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("提示模板页缺少 %q —— Prepare 注入的壳键或出口内容没渲染出来，长度 %d", want, len(body))
		}
	}

	if strings.Contains(body, "隐藏菜单项") {
		t.Fatal("隐藏菜单项不该出现在侧栏")
	}
}
