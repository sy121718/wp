// 本文件是**外部测试包**（adminctx_test）：它 import 真实 internal/adminui 来跑端到端链，
// 放包内会形成 adminctx → adminui 的测试依赖；外部测试包不参与生产依赖图，方向仍单向。
package adminctx_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/adminctx"
	admindto "go_wp/internal/module/admin/dto"
)

// sidebarStub 支持成功与权限查询错误路径；细分失败守卫在 context_test.go。
type sidebarStub struct {
	tree          []admindto.MenuTreeNode
	permissionErr error
}

func (s *sidebarStub) IsSuperAdmin(context.Context, uint64) (bool, error) { return false, nil }
func (s *sidebarStub) GetRoleCodesByUserID(context.Context, uint64) ([]string, error) {
	return nil, nil
}
func (s *sidebarStub) GetPermissionCodesByIDs(context.Context, []uint64) ([]string, error) {
	return nil, nil
}
func (s *sidebarStub) ListByCodes(context.Context, []string) ([]admindto.PermBrief, error) {
	return nil, nil
}
func (s *sidebarStub) ExistsEnabledCode(context.Context, string) (bool, error) { return false, nil }
func (s *sidebarStub) EffectivePermissionCodes(context.Context, uint64) ([]string, error) {
	return []string{"page:view"}, s.permissionErr
}
func (s *sidebarStub) AuthorizedButtonCodes(context.Context, []string) ([]string, error) {
	return []string{"page.edit"}, nil
}
func (s *sidebarStub) BuildAuthorizedTree(context.Context, []string) ([]admindto.MenuTreeNode, error) {
	return s.tree, nil
}

// TestMiddlewareTreeReachesAdminuiSidebar 钉住端到端链：中间件写入的授权树 → 真实
// adminui.Prepare → 侧栏视图（NavGroups）+ PermSet / Buttons 三个产物。
//
// 这条链上任何一处键名分歧（中间件写 nav_tree、Prepare 读别的键）或树未注入，Jet 都不会
// 报错 —— 侧栏只会静默少一块。三个断言合起来才封住这三种静默故障。
func TestMiddlewareErrorReachesPrepareAsEmptyPermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st := &sidebarStub{
		permissionErr: errors.New("permission query failed"),
		tree:          []admindto.MenuTreeNode{{ID: 1, Type: 2, Title: "Pages", Path: "/admin/pages"}},
	}
	var data gin.H
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", int64(7))
		c.Next()
	})
	r.Use(adminctx.PermContextMiddleware(st))
	r.GET("/admin/pages", func(c *gin.Context) {
		data = adminui.Prepare(c, nil)
		c.Status(http.StatusNoContent)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/admin/pages", nil))
	if data == nil {
		t.Fatal("handler was not reached")
	}
	for _, key := range []string{"PermSet", "Buttons"} {
		set, ok := data[key].(map[string]bool)
		if !ok || len(set) != 0 {
			t.Fatalf("%s must stay empty after permission error: %#v", key, data[key])
		}
	}
	groups, ok := data["NavGroups"].([]adminui.NavGroup)
	if !ok || len(groups) != 0 || data["HasSubnav"] != false {
		t.Fatalf("sidebar must stay empty after permission error: %#v", data)
	}
}

func TestMiddlewareTreeReachesAdminuiSidebar(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st := &sidebarStub{tree: []admindto.MenuTreeNode{{
		ID: 1, Type: 1, Title: "内容",
		Children: []admindto.MenuTreeNode{{ID: 2, Type: 2, Title: "文章", Path: "/admin/pages"}},
	}}}

	var data gin.H
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", int64(7))
		c.Next()
	})
	r.Use(adminctx.PermContextMiddleware(st))
	r.GET("/admin/pages", func(c *gin.Context) {
		data = adminui.Prepare(c, gin.H{"title": "页面"})
		c.Status(http.StatusNoContent)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/admin/pages", nil))

	if data == nil {
		t.Fatal("handler 未被到达：中间件没有 c.Next()")
	}
	groups, ok := data["NavGroups"].([]adminui.NavGroup)
	if !ok || len(groups) == 0 {
		t.Fatalf("侧栏没有渲染出目录：NavGroups=%#v（中间件与 Prepare 的 nav_tree 键名分歧，或树未注入）", data["NavGroups"])
	}
	if groups[0].Title != "内容" || len(groups[0].Nodes) != 1 || groups[0].Nodes[0].Path != "/admin/pages" {
		t.Fatalf("侧栏视图与桩树不一致：%#v", groups[0])
	}
	if !groups[0].Active || !groups[0].Nodes[0].Active {
		t.Fatalf("当前页 /admin/pages 未被标记高亮：%#v", groups[0])
	}
	if set, ok := data["PermSet"].(map[string]bool); !ok || !set["page:view"] {
		t.Fatalf("Prepare 没读到权限码集合：%#v", data["PermSet"])
	}
	if buttons, ok := data["Buttons"].(map[string]bool); !ok || !buttons["page.edit"] {
		t.Fatalf("Prepare 没读到按钮码集合：%#v", data["Buttons"])
	}
}
