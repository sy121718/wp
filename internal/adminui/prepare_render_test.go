package adminui

// 既有提示模板的真 Prepare + 真 Jet 契约：外壳键、导航与完整 HTML 必须一致。
// 认证拒绝已经迁成 JSON，此处直接验证仍在使用的渲染函数。

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

// prepareRenderNavTree 授权菜单树：一个目录组（当前页归属它 ⇒ 二级栏渲染）+ 直接链接项 + 隐藏项。
func prepareRenderNavTree() []admindto.MenuTreeNode {
	return []admindto.MenuTreeNode{
		{ID: 1, Title: "商品", Type: 1, Children: []admindto.MenuTreeNode{
			{ID: 2, Title: "商品列表", Path: "/admin/products", Type: 2},
		}},
		{ID: 3, Title: "仪表盘", Path: "/admin", Type: 2},
		{ID: 4, Title: "隐藏菜单项", Path: "/admin/hidden", Type: 2, IsHidden: 1},
	}
}

// TestJumpTemplateRendersFullShellWithRealPrepare 既有模板的整页渲染应包含完整外壳和提示内容。
func TestJumpTemplateRendersFullShellWithRealPrepare(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// 模板根相对包目录：internal/adminui → internal/templates。
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "templates"), true)
	engine.GET("/admin/products", func(c *gin.Context) {
		// 这三个上下文键在生产由 adminctx.PermContextMiddleware 写入（有效权限码 / 按钮码 / 授权菜单树）。
		// 这里直接种：本用例钉的是 Prepare → 出口 → 模板这条链，不是中间件链路
		//（中间件自身由 admin 契约测试与模块渲染测试覆盖）。
		c.Set(adminctx.PermSetKey, map[string]bool{"product.list": true})
		c.Set(adminctx.ButtonsKey, map[string]bool{"product.create": true})
		c.Set(adminctx.NavTreeKey, prepareRenderNavTree())
		RenderJumpPageWith(c, http.StatusOK, JumpView{OK: true, Msg: "已完成", Back: "/admin", BackText: "返回后台"}, Prepare)
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/products", nil)
	// 侧栏态走真 cookie 路径（固定态）。
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

	// Prepare 注入的壳键 + 出口自己的内容，逐项必须在整页里能看见。
	for _, want := range []string{
		`class="rail" id="adminRail"`,                    // 侧栏一级图标栏
		`data-group="1"`,                                 // NavGroups 真注入（侧栏数据驱动）
		`<nav class="subnav" id="subnav">`,               // HasSubnav（当前页归属目录分组）
		`<a class="rail-btn" href="/admin" title="仪表盘">`, // 直接链接形态的一级项
		`<html lang="`,                                   // 语言壳键
		`<meta name="csrf-token"`,                        // csrf_token（layout head）
		`X-CSRF-Token`,                                   // hx-headers
		`name="redirect"`,                                // lang_redirect 隐藏域
		`data-jump-state="ok"`,                           // 成功提示
		`class="jump-msg">已完成</p>`,                       // 受控文案（词条缺失回落原文）
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("提示模板页缺少 %q —— Prepare 注入的壳键或出口内容没渲染出来，长度 %d", want, len(body))
		}
	}

	// 隐藏菜单项不进侧栏（Prepare → BuildNav 的过滤，认了它就会出现「高亮指向看不见的菜单」）。
	if strings.Contains(body, "隐藏菜单项") {
		t.Fatal("隐藏菜单项不该出现在侧栏")
	}
}
