package adminctx_test

import (
	"context"
	"errors"
	jetpkg "go_wp/pkg/view/jet"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	view "go_wp/pkg/view"
	"go_wp/internal/middleware/adminctx"
	admindto "go_wp/internal/module/admin/dto"
)

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
func (s *sidebarStub) ListByPathMethod(context.Context, string, string) ([]admindto.PermBrief, error) {
	return nil, nil
}
func (s *sidebarStub) EffectivePermissionCodes(context.Context, uint64) ([]string, error) {
	return []string{"page:view"}, s.permissionErr
}
func (s *sidebarStub) AuthorizedButtonCodes(context.Context, []string) ([]string, error) {
	return []string{"page.edit"}, nil
}
func (s *sidebarStub) BuildAuthorizedTree(context.Context, []string) ([]admindto.MenuTreeNode, error) {
	return s.tree, nil
}

// navNodesForTest 复刻应用装配层的投影（routers.view_admin_port.go）：
// 上下文里的菜单树是模块 DTO，pkg/view 只接受自己的渲染事实类型。
// navItemsForTest 复刻应用装配层的菜单投影（routers.view_scope_port.go 的同一逻辑）。
func navItemsForTest(nodes []view.AdminNavNode) []jetpkg.NavItem {
	out := make([]jetpkg.NavItem, 0, len(nodes))
	for _, n := range nodes {
		if n.Hidden {
			continue
		}
		item := jetpkg.NavItem{ID: n.ID, Title: n.Title, TitleKey: n.TitleKey, Path: n.Path, Icon: n.Icon}
		item.Children = navItemsForTest(n.Children)
		if len(item.Children) == 0 && item.Path == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func navNodesForTest(nodes []admindto.MenuTreeNode) []view.AdminNavNode {
	out := make([]view.AdminNavNode, 0, len(nodes))
	for i := range nodes {
		n := &nodes[i]
		out = append(out, view.AdminNavNode{
			ID: n.ID, Title: n.Title, TitleKey: n.TitleKey,
			Path: n.Path, Icon: n.Icon, Hidden: n.IsHidden == 1,
			Children: navNodesForTest(n.Children),
		})
	}
	return out
}

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
		facts := view.PageFacts(c)
		var tree []admindto.MenuTreeNode
		if v, ok := c.Get(adminctx.NavTreeKey); ok {
			tree, _ = v.([]admindto.MenuTreeNode)
		}
		items := navItemsForTest(navNodesForTest(tree))
		data = gin.H{"PermSet": facts.PermSet, "Buttons": facts.Buttons,
			"NavItems": items, "HasSubnav": len(items) > 0}
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
	items, ok := data["NavItems"].([]jetpkg.NavItem)
	if !ok || len(items) != 0 || data["HasSubnav"] != false {
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
		facts := view.PageFacts(c)
		var tree []admindto.MenuTreeNode
		if v, ok := c.Get(adminctx.NavTreeKey); ok {
			tree, _ = v.([]admindto.MenuTreeNode)
		}
		items := navItemsForTest(navNodesForTest(tree))
		data = gin.H{"PermSet": facts.PermSet, "Buttons": facts.Buttons, "NavItems": items}
		c.Status(http.StatusNoContent)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/admin/pages", nil))

	if data == nil {
		t.Fatal("handler 未被到达：中间件没有 c.Next()")
	}
	items, ok := data["NavItems"].([]jetpkg.NavItem)
	if !ok || len(items) == 0 {
		t.Fatalf("侧栏没有渲染出目录：NavItems=%#v（中间件与 Prepare 的 nav_tree 键名分歧，或树未注入）", data["NavItems"])
	}
	if items[0].Title != "内容" || len(items[0].Children) != 1 || items[0].Children[0].Path != "/admin/pages" {
		t.Fatalf("侧栏视图与桩树不一致：%#v", items[0])
	}
	owner := jetpkg.NavOwner(items, "/admin/pages")
	if !jetpkg.NavIsOwner(items[0].Children[0], owner) || !jetpkg.NavBranchActive(items[0], owner) {
		t.Fatalf("当前页 /admin/pages 未被标记高亮：%#v", items[0])
	}
	if set, ok := data["PermSet"].(map[string]bool); !ok || !set["page:view"] {
		t.Fatalf("Prepare 没读到权限码集合：%#v", data["PermSet"])
	}
	if buttons, ok := data["Buttons"].(map[string]bool); !ok || !buttons["page.edit"] {
		t.Fatalf("Prepare 没读到按钮码集合：%#v", data["Buttons"])
	}
}
