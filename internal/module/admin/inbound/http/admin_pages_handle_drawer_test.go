package adminhttp

import (
	"bytes"
	"context"
	"errors"
	"github.com/CloudyKit/jet/v6"
	"go_wp/internal/web/shell"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	admincontract "go_wp/internal/module/admin/contract"
	admindto "go_wp/internal/module/admin/dto"
	"go_wp/internal/templates"
)

type drawerMenuService struct {
	admincontract.MenuService
	row       *admindto.MenuDetailResp
	updateErr error
}

func (s *drawerMenuService) MenuDetail(_ context.Context, req *admindto.MenuDetailReq) (*admindto.MenuDetailResp, error) {
	if s.row == nil || s.row.ID != req.ID {
		return nil, errors.New("menu missing")
	}
	return s.row, nil
}
func (s *drawerMenuService) MenuUpdate(_ context.Context, _ *admindto.MenuUpdateReq) error {
	return s.updateErr
}

func testDrawerContext(path string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	return c
}

type drawerPermService struct {
	admincontract.PermService
	row       *admindto.PermDetailResp
	updateErr error
}

func (s *drawerPermService) PermDetail(_ context.Context, req *admindto.PermDetailReq) (*admindto.PermDetailResp, error) {
	if s.row == nil || s.row.ID != req.ID {
		return nil, errors.New("permission missing")
	}
	return s.row, nil
}
func (s *drawerPermService) PermUpdate(_ context.Context, _ *admindto.PermUpdateReq) (*admindto.PermUpdateResp, error) {
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	return &admindto.PermUpdateResp{}, nil
}

// PermOptions 供菜单表单的权限点清单使用（迁移 470：一个菜单可以挂多个码）。
// 返回两条不同 module 的候选，顺带覆盖「分组」这条路径。
func (s *drawerPermService) PermOptions(_ context.Context, _ *admindto.PermOptionsReq) (*admindto.PermOptionsResp, error) {
	return &admindto.PermOptionsResp{List: []admindto.PermOptionItem{
		{ID: 1, PermissionCode: "menu:list", PermissionName: "菜单列表", Module: "menu"},
		{ID: 2, PermissionCode: "menu:create", PermissionName: "新建菜单", Module: "menu"},
		{ID: 3, PermissionCode: "role:list", PermissionName: "角色列表", Module: "role"},
	}}, nil
}

func TestPermissionEditFragmentAndEcho(t *testing.T) {
	s := &drawerPermService{row: &admindto.PermDetailResp{ID: 42, PermissionCode: "permission:demo", PermissionName: "原权限", Module: "admin", APIPath: "/api/demo", APIMethod: "POST", Status: 1}}
	h := &AdminPagesHandle{perms: s}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	r.GET("/permissions/edit", h.PermissionsEditFragment)
	r.POST("/permissions/update", h.PermissionsUpdate)
	for _, tc := range []struct {
		name, query string
		status      int
		fragment    bool
	}{
		{"found", "42", 200, true}, {"invalid", "bad", 400, false}, {"missing", "43", 404, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/permissions/edit?id="+tc.query, nil))
			if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("response: %d %v %s", w.Code, w.Header(), w.Body.String())
			}
			if tc.fragment {
				body := strings.TrimSpace(w.Body.String())
				if !strings.HasPrefix(body, "<div data-drawer-fragment>") || !strings.HasSuffix(body, "</div>") || !strings.Contains(body, "name=\"permission_code\"") || !strings.Contains(body, "保存修改") {
					t.Fatalf("shape: %s", body)
				}
			}
		})
	}
	s.updateErr = errors.New("private database detail")
	form := url.Values{"id": {"42"}, "permission_code": {"permission:demo"}, "permission_name": {"用户原值"}, "module": {"admin"}, "api_path": {"/custom"}, "api_method": {"POST"}, "status": {"1"}}
	request := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/permissions/update", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		return req
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, request())
	if w.Code != 200 || w.Header().Get("HX-Redirect") != "" || !strings.Contains(w.Body.String(), "用户原值") || !strings.Contains(w.Body.String(), "role=\"alert\"") || strings.Contains(w.Body.String(), "private database") {
		t.Fatalf("fail: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	s.updateErr = nil
	w = httptest.NewRecorder()
	r.ServeHTTP(w, request())
	if w.Header().Get("HX-Redirect") != "/admin/permissions" {
		t.Fatalf("success: %v", w.Header())
	}
}

func TestMenuEditFragmentAndEcho(t *testing.T) {
	s := &drawerMenuService{row: &admindto.MenuDetailResp{ID: 42, Title: "原菜单", Type: 2, Status: 1, PermissionCodes: []string{"menu:list"}}}
	h := &AdminPagesHandle{menus: s, perms: &drawerPermService{}}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	set := jet.NewSet(jet.NewOSFileSystemLoader("../../../../templates"), jet.WithTemplateNameExtensions([]string{"", ".html"}))
	for _, data := range []gin.H{
		shell.Prepare(testDrawerContext("/menus/edit"), gin.H{"MenuEdit": s.row}),
		shell.Prepare(testDrawerContext("/menus/update"), gin.H{"MenuEdit": s.row, "MenuEditErr": "错误", "MenuEditEcho": map[string]string{"title": "用户原值", "parent_id": "0", "type": "2", "status": "1", "path": "/custom", "icon": "", "sort_order": "0", "remark": ""}}),
		// 权限点清单：候选按 module 分组，勾选态在 Go 侧算好（模板只渲染）。
		shell.Prepare(testDrawerContext("/menus/edit"), gin.H{"MenuEdit": s.row, "PermChoices": []permChoiceGroup{
			{Module: "menu", Items: []permChoiceItem{{Code: "menu:list", Name: "菜单列表", Checked: true}, {Code: "menu:create", Name: "新建菜单"}}},
			{Module: "role", Items: []permChoiceItem{{Code: "role:list", Name: "角色列表"}}},
		}}),
	} {
		tpl, err := set.GetTemplate("admin/system/menu_edit_form.html")
		if err != nil {
			t.Fatal(err)
		}
		var rendered bytes.Buffer
		if err := tpl.Execute(&rendered, nil, data); err != nil {
			t.Fatalf("template: %v", err)
		}
	}
	r.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	r.GET("/menus/edit", h.MenusEditFragment)
	r.POST("/menus/update", h.MenusUpdate)
	for _, tc := range []struct {
		name, query string
		status      int
		fragment    bool
	}{
		{"found", "42", 200, true}, {"invalid", "x", 400, false},
		{"missing", "43", 404, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/menus/edit?id="+tc.query, nil))
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("fragment must not be cached")
			}
			if tc.fragment {
				body := strings.TrimSpace(w.Body.String())
				if !strings.HasPrefix(body, "<div data-drawer-fragment>") || !strings.HasSuffix(body, "</div>") || !strings.Contains(body, "<form") || strings.Contains(body, "<html") {
					t.Fatalf("fragment shape: %s", body)
				}
				// 权限点清单必须真的在片段里：它是「菜单能配权限」的唯一入口，
				// 而缺 PermChoices 时模板会静默跳过整块（不做反向断言就会漏掉这个退路）。
				if !strings.Contains(body, `name="permission_codes" value="menu:list" checked`) ||
					!strings.Contains(body, `name="permission_codes" value="menu:create"`) {
					t.Fatalf("perm picker missing: %s", body)
				}
			}
		})
	}
	s.updateErr = errors.New("backend detail should not leak")
	form := url.Values{"id": {"42"}, "title": {"用户原值"}, "type": {"2"}, "status": {"1"}, "path": {"/custom"}}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/menus/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	r.ServeHTTP(w, req)
	if w.Code != 200 || w.Header().Get("HX-Redirect") != "" || !strings.Contains(w.Body.String(), "用户原值") || !strings.Contains(w.Body.String(), "role=\"alert\"") || strings.Contains(w.Body.String(), "backend detail") {
		t.Fatalf("failure: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	s.updateErr = nil
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/menus/update", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	r.ServeHTTP(w, req)
	if w.Header().Get("HX-Redirect") != "/admin/menus" {
		t.Fatalf("success: %v", w.Header())
	}
}
