package adminhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/internal/templates"
	"go_wp/pkg/view"
)

// 浏览器只替换业务服务和会话存储；Controller、Jet、CSRF 与前端资产均使用真实实现。
type browserDrawerCSRF struct{}

func (browserDrawerCSRF) Get(*gin.Context) string         { return "browser-csrf" }
func (browserDrawerCSRF) Save(*gin.Context, string) error { return nil }

type browserMenuService struct {
	drawerMenuService
	mu sync.Mutex
}

func (s *browserMenuService) MenuDetail(ctx context.Context, req *admindto.MenuDetailReq) (*admindto.MenuDetailResp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.drawerMenuService.MenuDetail(ctx, req)
}

func (s *browserMenuService) MenuUpdate(_ context.Context, req *admindto.MenuUpdateReq) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req.Remark == "reject" {
		return adminenums.Err(adminenums.ErrMenuCircle)
	}
	s.updated = req
	row := *s.row
	row.Title, row.Status, row.Remark = req.Title, req.Status, req.Remark
	row.PermissionCodes = req.PermissionCodes
	s.row = &row
	return nil
}

type browserPermService struct {
	drawerPermService
	mu sync.Mutex
}

func (s *browserPermService) PermDetail(ctx context.Context, req *admindto.PermDetailReq) (*admindto.PermDetailResp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.drawerPermService.PermDetail(ctx, req)
}

func (s *browserPermService) PermUpdate(_ context.Context, req *admindto.PermUpdateReq) (*admindto.PermUpdateResp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req.Remark == "reject" {
		return nil, adminenums.Err(adminenums.ErrCodeImmutable)
	}
	s.updated = req
	row := *s.row
	row.PermissionName, row.Status, row.Remark = req.PermissionName, req.Status, req.Remark
	s.row = &row
	return &admindto.PermUpdateResp{}, nil
}

func TestAdminGrantDrawersRealBrowser(t *testing.T) {
	if os.Getenv("GOWP_HTML_UI_BROWSER") != "1" {
		t.Skip("设置 GOWP_HTML_UI_BROWSER=1 验证真实授权抽屉")
	}
	svc := newGrantDrawerService()
	h := NewAdminPagesHandle(svc, svc, nil, svc, nil, nil)
	api := NewHandleWithDeps(svc, svc, nil, nil, nil, nil)
	r := drawerEngine()
	r.GET("/", view.HTMLPage, func(c *gin.Context) {
		c.HTML(http.StatusOK, "testdata/html-ui/admin-grant-drawers.html", nil)
	})
	r.GET("/static/js/*name", func(c *gin.Context) {
		source, err := templates.StaticJS(c.Param("name")[1:])
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "text/javascript; charset=utf-8", []byte(source))
	})
	r.GET("/static/css/ui.css", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/css; charset=utf-8", []byte(templates.UICSS()))
	})
	r.GET("/admin/administrators/roles/drawer", view.HTMLFragment, h.AdministratorRolesDrawer)
	r.GET("/admin/administrators/menus/drawer", view.HTMLFragment, h.AdministratorMenusDrawer)
	r.GET("/admin/roles/permissions/drawer", view.HTMLFragment, h.RolePermissionsDrawer)
	write := r.Group("/api", builtin.CSRFMiddlewareWith(browserDrawerCSRF{}), func(c *gin.Context) { c.Set("user_id", int64(123)) })
	write.POST("/admin/role/save", view.HTMLFragment, api.AdminRoleSave)
	write.POST("/admin/menu/save", view.HTMLFragment, api.AdminMenuSave)
	write.POST("/role/menu/save", view.HTMLFragment, api.RoleMenuSave)
	server := httptest.NewServer(r)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "--test", "../../../../templates/testdata/html-ui/admin-grant-drawers.cjs")
	cmd.Env = append(os.Environ(), "GOWP_HTML_UI_BASE_URL="+server.URL)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("真实授权抽屉验收失败：%v\n%s", err, output)
	}
	t.Logf("%s", output)
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if svc.operator != 123 || svc.target != 7 || len(svc.roleCodes) != 0 || len(svc.adminIDs) != 0 || len(svc.roleIDs) != 0 || svc.calls != 27 {
		t.Fatalf("浏览器授权保存事实错误：operator=%d target=%d roles=%v adminMenus=%v roleMenus=%v calls=%d", svc.operator, svc.target, svc.roleCodes, svc.adminIDs, svc.roleIDs, svc.calls)
	}
}

func TestAdminDrawersRealBrowser(t *testing.T) {
	if os.Getenv("GOWP_HTML_UI_BROWSER") != "1" {
		t.Skip("设置 GOWP_HTML_UI_BROWSER=1 验证真实系统编辑抽屉")
	}
	menus := &browserMenuService{drawerMenuService: drawerMenuService{
		row:     &admindto.MenuDetailResp{ID: 42, Title: "原菜单", ParentID: 7, Type: 2, Path: "/admin/demo", Status: 0, PermissionCodes: []string{"menu:list"}},
		parents: []admindto.MenuParentChoice{{ID: 7, Title: "目录", Type: 1}, {ID: 42, Title: "原菜单", Type: 2, Disabled: true}},
	}}
	perms := &browserPermService{drawerPermService: drawerPermService{
		row: &admindto.PermDetailResp{ID: 42, PermissionCode: "permission:demo", PermissionName: "原权限", Module: "admin", APIPath: "/api/demo", APIMethod: "POST", Status: 0},
	}}
	h := &AdminPagesHandle{menus: menus, perms: perms}
	r := drawerEngine()
	r.GET("/", view.HTMLPage, func(c *gin.Context) {
		c.HTML(http.StatusOK, "testdata/html-ui/admin-drawers.html", nil)
	})
	r.GET("/static/js/*name", func(c *gin.Context) {
		source, err := templates.StaticJS(c.Param("name")[1:])
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "text/javascript; charset=utf-8", []byte(source))
	})
	r.GET("/static/css/ui.css", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/css; charset=utf-8", []byte(templates.UICSS()))
	})
	r.GET("/admin/menus/edit", view.HTMLFragment, h.MenusEditFragment)
	r.GET("/admin/permissions/edit", view.HTMLFragment, h.PermissionsEditFragment)
	write := r.Group("/admin", builtin.CSRFMiddlewareWith(browserDrawerCSRF{}))
	write.POST("/menus/update", view.HTMLFragment, h.MenusUpdate)
	write.POST("/permissions/update", view.HTMLFragment, h.PermissionsUpdate)
	server := httptest.NewServer(r)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "--test", "../../../../templates/testdata/html-ui/admin-drawers.cjs")
	cmd.Env = append(os.Environ(), "GOWP_HTML_UI_BASE_URL="+server.URL)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("真实系统抽屉验收失败：%v\n%s", err, output)
	}
	t.Logf("%s", output)
	menus.mu.Lock()
	defer menus.mu.Unlock()
	perms.mu.Lock()
	defer perms.mu.Unlock()
	if menus.updated == nil || len(menus.updated.PermissionCodes) != 0 || menus.updated.Title != "菜单 375" || menus.updated.Status != 0 {
		t.Fatalf("菜单提交事实错误：%+v", menus.updated)
	}
	if perms.updated == nil || perms.updated.PermissionName != "权限 375" || perms.updated.Status != 0 {
		t.Fatalf("权限提交事实错误：%+v", perms.updated)
	}
}
