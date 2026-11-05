package adminhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	admincontract "go_wp/internal/module/admin/contract"
	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/internal/permission"
	"go_wp/pkg/view"
)

// grantDrawerService 只替换 Service；读取与写入均经过真实 Controller、binder 和 Jet。
type grantDrawerService struct {
	admincontract.AdminService
	admincontract.RoleService
	admincontract.MenuService
	mu        sync.Mutex
	roleCodes []string
	adminIDs  []uint64
	roleIDs   []uint64
	operator  uint64
	target    uint64
	calls     int
	err       error
	empty     bool
}

func newGrantDrawerService() *grantDrawerService {
	return &grantDrawerService{roleCodes: []string{"editor"}, adminIDs: []uint64{3}, roleIDs: []uint64{1, 2, 3}}
}

func (s *grantDrawerService) AdminDetail(_ context.Context, req *admindto.AdminDetailReq) (*admindto.AdminDetailResp, error) {
	if req.Id != 42 {
		return nil, adminenums.Err(adminenums.ErrAdminNotFound)
	}
	return &admindto.AdminDetailResp{ID: 42, Username: "tester"}, nil
}

func (s *grantDrawerService) RoleList(context.Context, *admindto.RoleListReq) (*admindto.RoleListResp, error) {
	if s.empty {
		return &admindto.RoleListResp{}, nil
	}
	return &admindto.RoleListResp{Total: 3, List: []admindto.RoleItem{
		{RoleCode: "editor", RoleName: `编辑<script>`, Status: 1},
		{RoleCode: "disabled", RoleName: "禁用角色", Status: 0},
		{RoleCode: "reject", RoleName: "拒绝保存", Status: 1},
	}}, nil
}

func (s *grantDrawerService) AdminRoleList(context.Context, *admindto.AdminRoleListReq) (*admindto.AdminRoleListResp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res := &admindto.AdminRoleListResp{}
	for _, code := range s.roleCodes {
		res.List = append(res.List, admindto.AdminRoleListItem{RoleCode: code})
	}
	return res, nil
}

func (s *grantDrawerService) MenuTree(context.Context, *admindto.MenuTreeReq) ([]admindto.MenuTreeNode, error) {
	if s.empty {
		return nil, nil
	}
	return []admindto.MenuTreeNode{{ID: 1, Title: "目录", Type: 1, Children: []admindto.MenuTreeNode{
		{ID: 2, ParentID: 1, Title: "继承菜单", Type: 2, PermissionCodes: []string{"grant:inherited"}},
		{ID: 3, ParentID: 1, Title: `直接菜单<script>`, Type: 3, PermissionCodes: []string{"grant:direct"}},
		{ID: 4, ParentID: 1, Title: "拒绝保存", Type: 5},
	}}}, nil
}

func (s *grantDrawerService) AdminMenuList(context.Context, *admindto.AdminMenuListReq) (*admindto.AdminMenuListResp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &admindto.AdminMenuListResp{DirectMenuIDs: slices.Clone(s.adminIDs), EffectiveMenuIDs: append([]uint64{1, 2}, s.adminIDs...)}, nil
}

func (s *grantDrawerService) RolePermissionTree(ctx context.Context, roleID uint64) (*admindto.RolePermissionTreeResp, error) {
	tree, err := s.MenuTree(ctx, nil)
	s.mu.Lock()
	defer s.mu.Unlock()
	return &admindto.RolePermissionTreeResp{RoleID: roleID, RoleCode: "editor", MenuIDs: slices.Clone(s.roleIDs), Tree: tree}, err
}

func (s *grantDrawerService) AdminRoleSave(_ context.Context, req *admindto.AdminRoleSaveReq) (*admindto.AdminRoleSaveResp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.target, s.operator = req.UserID, req.OperatorID
	if s.err != nil {
		return nil, s.err
	}
	if slices.Contains(req.RoleCodes, "reject") {
		return nil, adminenums.Err(adminenums.ErrCodeImmutable)
	}
	s.roleCodes = slices.Clone(req.RoleCodes)
	return &admindto.AdminRoleSaveResp{UserID: req.UserID}, nil
}

func (s *grantDrawerService) AdminMenuSave(_ context.Context, req *admindto.AdminMenuSaveReq) (*admindto.AdminMenuSaveResp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.target, s.operator = req.UserID, req.OperatorID
	if s.err != nil {
		return nil, s.err
	}
	if slices.Contains(req.MenuIDs, 4) {
		return nil, adminenums.Err(adminenums.ErrCodeImmutable)
	}
	s.adminIDs = slices.Clone(req.MenuIDs)
	return &admindto.AdminMenuSaveResp{UserID: req.UserID}, nil
}

func (s *grantDrawerService) RoleMenuSave(_ context.Context, req *admindto.RoleMenuSaveReq) (*admindto.RoleMenuSaveResp, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.target, s.operator = req.RoleID, req.OperatorID
	if s.err != nil {
		return nil, s.err
	}
	if slices.Contains(req.MenuIDs, 4) {
		return nil, adminenums.Err(adminenums.ErrCodeImmutable)
	}
	s.roleIDs = slices.Clone(req.MenuIDs)
	return &admindto.RoleMenuSaveResp{}, nil
}

func TestGrantDrawersRenderJetFacts(t *testing.T) {
	s := newGrantDrawerService()
	s.roleCodes = append(s.roleCodes, "unknown")
	h := NewAdminPagesHandle(s, s, nil, s, nil, nil)
	r := drawerEngine()
	r.GET("/roles", view.HTMLFragment, h.AdministratorRolesDrawer)
	r.GET("/menus", view.HTMLFragment, h.AdministratorMenusDrawer)
	r.GET("/permissions", view.HTMLFragment, h.RolePermissionsDrawer)
	for _, tc := range []struct {
		path string
		want []string
	}{
		{"/roles?user_id=42", []string{`name="role_codes" value="editor" checked`, `value="unknown" checked`, "未知角色", "禁用角色", "&lt;script&gt;"}},
		{"/menus?user_id=42", []string{`data-id="2" data-parent="1" data-depth="2" checked disabled`, "来自角色", "grant:direct", "&lt;script&gt;", "padding-left:30px"}},
		{"/permissions?role_id=7", []string{`data-id="2" data-parent="1" data-depth="2" checked>`, "grant:direct", `name="role_id" value="7"`}},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
		body := assertDrawerHTML(t, w, http.StatusOK)
		for _, want := range append(tc.want, "data-drawer-fragment", "data-write-reload", "</form>") {
			if !strings.Contains(body, want) {
				t.Errorf("%s 缺少 %q: %s", tc.path, want, body)
			}
		}
		if strings.Contains(body, "<script>") {
			t.Fatal("业务事实未转义")
		}
	}
}

func TestGrantListOpenersRenderHTMLProtocol(t *testing.T) {
	for _, domain := range []string{"administrators", "roles"} {
		t.Run(domain, func(t *testing.T) {
			r := drawerEngine()
			r.GET("/", view.HTMLPage, func(c *gin.Context) {
				data := gin.H{
					"t": func(_ string, fallback string) string { return fallback }, "csrf_token": "test-csrf",
					"Buttons": map[string]bool{"admin.role_list": true, "admin.menu_list": true, "role.menu_list": true},
					"Total":   int64(1), "FilterName": "", "FilterEmail": "", "FilterKeyword": "",
				}
				if domain == "administrators" {
					data["Rows"] = []admindto.AdminItem{{ID: 42, Username: "tester"}}
				} else {
					data["Rows"] = []admindto.RoleItem{{ID: 42, RoleCode: "editor", RoleName: "编辑"}}
				}
				c.HTML(http.StatusOK, "admin/system/"+domain+".html", data)
			})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
			body := w.Body.String()
			if w.Code != http.StatusOK || !strings.Contains(body, "<html") {
				t.Fatalf("真实列表模板失败: %d %s", w.Code, body)
			}
			paths := []string{"administrators/roles/drawer?user_id=42", "administrators/menus/drawer?user_id=42"}
			if domain == "roles" {
				paths = []string{"roles/permissions/drawer?role_id=42"}
			}
			for _, path := range paths {
				if strings.Count(body, `data-drawer-open hx-get="/admin/`+path+`" hx-target="#admin-drawer-body" hx-swap="innerHTML"`) != 1 {
					t.Fatalf("列表必须声明远程 HTML 抽屉 %s: %s", path, body)
				}
			}
			for _, retired := range []string{"data-drawer-url", "data-drawer-template", "data-bind", "tpl-admin-role-drawer", "tpl-admin-menu-drawer", "tpl-role-perm-drawer"} {
				if strings.Contains(body, retired) {
					t.Fatalf("列表仍含旧授权回填 %s", retired)
				}
			}
		})
	}
}

func TestGrantDrawersEmptyCatalogHasNoSave(t *testing.T) {
	s := &grantDrawerService{empty: true}
	h := NewAdminPagesHandle(s, s, nil, s, nil, nil)
	r := drawerEngine()
	r.GET("/roles", view.HTMLFragment, h.AdministratorRolesDrawer)
	r.GET("/menus", view.HTMLFragment, h.AdministratorMenusDrawer)
	r.GET("/permissions", view.HTMLFragment, h.RolePermissionsDrawer)
	for _, path := range []string{"/roles?user_id=42", "/menus?user_id=42", "/permissions?role_id=7"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		body := assertDrawerHTML(t, w, http.StatusOK)
		if !strings.Contains(body, "empty-state") || strings.Contains(body, `type="submit"`) {
			t.Fatalf("空目录不得提交清空授权: %s", body)
		}
	}
}

func TestGrantSavesUseFormBodyAndTrustedOperator(t *testing.T) {
	for _, domain := range []string{"admin-role", "admin-menu", "role-menu"} {
		t.Run(domain, func(t *testing.T) {
			for _, scenario := range []string{"selected", "clear", "missing", "query-only", "invalid", "unauthorized", "failure"} {
				t.Run(scenario, func(t *testing.T) {
					s := newGrantDrawerService()
					h := NewHandleWithDeps(s, s, nil, nil, nil, nil)
					handler, idName, field, value := h.AdminRoleSave, "user_id", "role_codes", "disabled"
					if domain == "admin-menu" {
						handler, field, value = h.AdminMenuSave, "menu_ids", "3"
					} else if domain == "role-menu" {
						handler, idName, field, value = h.RoleMenuSave, "role_id", "menu_ids", "3"
					}
					form := url.Values{idName: {"42"}, field: {value}, "OperatorID": {"999"}, "operator_id": {"999"}}
					status := http.StatusOK
					switch scenario {
					case "clear":
						form.Del(field)
					case "missing", "query-only":
						form.Del(idName)
						status = http.StatusBadRequest
					case "invalid":
						form.Set(idName, "bad")
						status = http.StatusBadRequest
					case "unauthorized":
						status = http.StatusUnauthorized
					case "failure":
						s.err = errors.New("SQLSTATE private")
						status = http.StatusInternalServerError
					}
					r := drawerEngine()
					r.POST("/save", view.HTMLFragment, func(c *gin.Context) {
						if scenario != "unauthorized" {
							c.Set("user_id", int64(123))
						}
					}, handler)
					path := "/save"
					if scenario == "query-only" {
						path += "?" + idName + "=42"
					}
					req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
					req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					w := httptest.NewRecorder()
					r.ServeHTTP(w, req)
					assertDrawerHTML(t, w, status)
					called := status == http.StatusOK || scenario == "failure"
					if (s.calls == 1) != called || (called && (s.target != 42 || s.operator != 123)) {
						t.Fatalf("调用与可信身份错误: calls=%d target=%d operator=%d", s.calls, s.target, s.operator)
					}
					if status == http.StatusOK {
						var count int
						switch domain {
						case "admin-role":
							count = len(s.roleCodes)
						case "admin-menu":
							count = len(s.adminIDs)
						case "role-menu":
							count = len(s.roleIDs)
						}
						if (count == 0) != (scenario == "clear") {
							t.Fatalf("完整替换或清空未交服务: count=%d", count)
						}
					}
				})
			}
		})
	}
}

func TestGrantSaveRoutesDeclareHTMLBeforeSession(t *testing.T) {
	r := drawerEngine()
	SetupAdminRoutes(permission.NewRouteGroup(r.Group("/api")), nil)
	for _, path := range []string{"/api/admin/role/save", "/api/admin/menu/save", "/api/role/menu/save"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set("Accept", "application/json")
		r.ServeHTTP(w, req)
		assertDrawerHTML(t, w, http.StatusUnauthorized)
	}
}

func TestGrantDTOsDoNotBindOperator(t *testing.T) {
	for _, req := range []any{&admindto.AdminRoleSaveReq{}, &admindto.AdminMenuSaveReq{}, &admindto.RoleMenuSaveReq{}} {
		c := testDrawerContext("/save")
		c.Request = httptest.NewRequest(http.MethodPost, "/save", strings.NewReader("user_id=42&role_id=7&OperatorID=999&operator_id=999"))
		c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if err := c.ShouldBindWith(req, ginbinding.FormPost); err != nil {
			t.Fatal(err)
		}
		if reflect.ValueOf(req).Elem().FieldByName("OperatorID").Uint() != 0 {
			t.Fatalf("可信操作人被原生 binder 绑定: %+v", req)
		}
	}
}
