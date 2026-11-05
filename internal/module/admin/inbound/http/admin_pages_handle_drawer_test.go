package adminhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	admincontract "go_wp/internal/module/admin/contract"
	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/internal/templates"
	"go_wp/internal/text"
	"go_wp/pkg/jsonx"
	"go_wp/pkg/view"
)

type drawerMenuService struct {
	admincontract.MenuService
	row       *admindto.MenuDetailResp
	detailErr error
	updateErr error
	updated   *admindto.MenuUpdateReq

	parents    []admindto.MenuParentChoice
	parentsErr error
}

func (s *drawerMenuService) MenuParentOptions(_ context.Context, _ uint64) ([]admindto.MenuParentChoice, error) {
	return s.parents, s.parentsErr
}

func (s *drawerMenuService) MenuDetail(_ context.Context, req *admindto.MenuDetailReq) (*admindto.MenuDetailResp, error) {
	if s.detailErr != nil {
		return nil, s.detailErr
	}
	if s.row == nil || s.row.ID != req.ID {
		return nil, adminenums.Err(adminenums.ErrMenuNotFound)
	}
	return s.row, nil
}
func (s *drawerMenuService) MenuUpdate(_ context.Context, req *admindto.MenuUpdateReq) error {
	s.updated = req
	return s.updateErr
}

func testDrawerContext(path string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	return c
}

type drawerPermService struct {
	admincontract.PermService
	row        *admindto.PermDetailResp
	detailErr  error
	updateErr  error
	optionsErr error
	updated    *admindto.PermUpdateReq
}

func (s *drawerPermService) PermDetail(_ context.Context, req *admindto.PermDetailReq) (*admindto.PermDetailResp, error) {
	if s.detailErr != nil {
		return nil, s.detailErr
	}
	if s.row == nil || s.row.ID != req.ID {
		return nil, adminenums.Err(adminenums.ErrPermissionNotFound)
	}
	return s.row, nil
}
func (s *drawerPermService) PermUpdate(_ context.Context, req *admindto.PermUpdateReq) (*admindto.PermUpdateResp, error) {
	s.updated = req
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	return &admindto.PermUpdateResp{}, nil
}

func (s *drawerPermService) PermOptions(_ context.Context, _ *admindto.PermOptionsReq) (*admindto.PermOptionsResp, error) {
	return &admindto.PermOptionsResp{List: []admindto.PermOptionItem{
		{ID: 1, PermissionCode: "menu:list", PermissionName: "菜单列表", Module: "menu"},
		{ID: 2, PermissionCode: "menu:create", PermissionName: "新建菜单", Module: "menu"},
		{ID: 3, PermissionCode: "role:list", PermissionName: "角色列表", Module: "role"},
	}}, s.optionsErr
}

func drawerEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	r.Use(view.FailureMiddleware(adminui.RenderFailure))
	r.Use(view.UseSuccess(adminui.NewSuccessWriter()))
	r.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) { view.FailScene(c, "template", err) }))
	r.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	return r
}

func assertDrawerHTML(t *testing.T, w *httptest.ResponseRecorder, status int) string {
	t.Helper()
	body := w.Body.String()
	if w.Code != status || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("抽屉协议错误：%d %v %s", w.Code, w.Header(), body)
	}
	if w.Header().Get("Location") != "" || w.Header().Get("HX-Redirect") != "" || strings.Contains(body, "<html") {
		t.Fatalf("局部响应不能导航或携带整页：%s", body)
	}
	if status >= 400 && (!strings.Contains(body, "data-ui-error") || strings.Contains(body, "<form")) {
		t.Fatalf("失败只能返回错误片段：%s", body)
	}
	for _, retired := range []string{"data-bind", "data-write-url", "catalog unavailable", "SQLSTATE private"} {
		if strings.Contains(body, retired) {
			t.Fatalf("抽屉包含旧回填指令或内部原文 %q：%s", retired, body)
		}
	}
	return body
}

func TestPermissionEditFragmentReturnsHTML(t *testing.T) {
	s := &drawerPermService{row: &admindto.PermDetailResp{ID: 42, PermissionCode: "permission:demo", PermissionName: `原权限"><script>`, Module: "admin", APIPath: "/api/demo", APIMethod: "POST", Status: 0}}
	h := &AdminPagesHandle{perms: s}
	r := drawerEngine()
	r.GET("/permissions/edit", view.HTMLFragment, h.PermissionsEditFragment)
	for _, tc := range []struct {
		name, query string
		status      int
	}{
		{"found", "42", http.StatusOK}, {"invalid", "bad", http.StatusBadRequest}, {"missing", "43", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/permissions/edit?id="+tc.query, nil))
			body := assertDrawerHTML(t, w, tc.status)
			if tc.name != "found" {
				return
			}
			for _, want := range []string{`data-drawer-fragment`, `hx-post="/admin/permissions/update"`, `name="id" value="42"`, `value="permission:demo"`, `value="admin"`, `value="/api/demo"`, `<option value="POST" selected>`, `<option value="0" selected>`, `&lt;script&gt;`, `data-write-status`, `data-write-reload`} {
				if !strings.Contains(body, want) {
					t.Errorf("权限抽屉缺少 %q：%s", want, body)
				}
			}
			if strings.Contains(body, "<script>") {
				t.Fatal("业务字段未转义")
			}
		})
	}
	s.detailErr = errors.New("SQLSTATE private")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/permissions/edit?id=42", nil))
	assertDrawerHTML(t, w, http.StatusInternalServerError)
}

func TestMenuEditFragmentReturnsHTML(t *testing.T) {
	s := &drawerMenuService{
		row: &admindto.MenuDetailResp{ID: 42, Title: "原菜单", ParentID: 7, Type: 4, Status: 0, SortOrder: -2, Icon: "menu", PermissionCodes: []string{"menu:list"}},
		parents: []admindto.MenuParentChoice{
			{ID: 7, Title: "目录", Type: 1},
			{ID: 42, Title: "原菜单", Type: 2, Indent: "　", Disabled: true},
		},
	}
	perms := &drawerPermService{}
	h := &AdminPagesHandle{menus: s, perms: perms}
	r := drawerEngine()
	r.GET("/menus/edit", view.HTMLFragment, h.MenusEditFragment)
	for _, tc := range []struct {
		name, query string
		status      int
	}{
		{"found", "42", http.StatusOK}, {"invalid", "x", http.StatusBadRequest}, {"missing", "43", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/menus/edit?id="+tc.query, nil))
			body := assertDrawerHTML(t, w, tc.status)
			if tc.name != "found" {
				return
			}
			for _, want := range []string{`data-drawer-fragment`, `hx-post="/admin/menus/update"`, `name="id" value="42"`, `value="原菜单"`, `<option value="7" data-type="1" selected>`, `<option value="42" data-type="2" disabled>`, `<option value="4" selected>`, `<option value="0" selected>`, `name="sort_order" class="form-input" value="-2"`, `name="icon" value="menu"`, `name="permission_codes" value="menu:list" checked`, `name="permission_codes" value="menu:create">`, `perm-picker-group-title">role</p>`} {
				if !strings.Contains(body, want) {
					t.Errorf("菜单抽屉缺少 %q：%s", want, body)
				}
			}
		})
	}
	// 父候选或权限目录读取失败必须拒绝整个片段，不能以空清单假成功。
	for _, source := range []string{"parents", "permissions", "detail"} {
		t.Run(source, func(t *testing.T) {
			s.parentsErr, perms.optionsErr, s.detailErr = nil, nil, nil
			switch source {
			case "parents":
				s.parentsErr = errors.New("catalog unavailable")
			case "permissions":
				perms.optionsErr = errors.New("catalog unavailable")
			case "detail":
				s.detailErr = errors.New("SQLSTATE private")
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/menus/edit?id=42", nil))
			assertDrawerHTML(t, w, http.StatusInternalServerError)
		})
	}
}

func TestDrawerUpdatesUseFormBodyAndHTML(t *testing.T) {
	titleKey := "menu.title"
	menus := &drawerMenuService{row: &admindto.MenuDetailResp{ID: 42, TitleKey: &titleKey, ExternalURL: "https://example.test/kept", IsHidden: 1, IsPublic: 1}}
	perms := &drawerPermService{}
	h := &AdminPagesHandle{menus: menus, perms: perms}
	r := drawerEngine()
	r.POST("/menus/update", view.HTMLFragment, h.MenusUpdate)
	r.POST("/permissions/update", view.HTMLFragment, h.PermissionsUpdate)
	menuForm := url.Values{"id": {"42"}, "title": {"修改菜单"}, "parent_id": {"0"}, "type": {"2"}, "status": {"0"}, "sort_order": {"0"}, "path": {""}, "remark": {""}, "icon": {""}, "permission_codes": {"menu:list", "menu:create"}, "title_key": {"forged"}, "external_url": {"https://evil.test/"}, "is_hidden": {"0"}, "is_public": {"0"}}
	permForm := url.Values{"id": {"42"}, "permission_code": {"permission:demo"}, "permission_name": {"修改权限"}, "module": {"admin"}, "api_path": {"/api/demo"}, "api_method": {"POST"}, "status": {"0"}, "remark": {""}}
	post := func(path string, form url.Values) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path+"?id=999&title=query&permission_name=query&permission_codes=forged", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for _, path := range []string{"/menus/update", "/permissions/update"} {
		form := menuForm
		if path == "/permissions/update" {
			form = permForm
		}
		body := assertDrawerHTML(t, post(path, form), http.StatusOK)
		if !strings.Contains(body, `role="status"`) || strings.Contains(body, "<form") {
			t.Fatalf("成功应是独立确认片段：%s", body)
		}
		for _, field := range []string{"id", "status"} {
			invalid := url.Values{}
			for key, value := range form {
				invalid[key] = value
			}
			invalid.Set(field, "bad")
			menus.updated, perms.updated = nil, nil
			assertDrawerHTML(t, post(path, invalid), http.StatusBadRequest)
			if menus.updated != nil || perms.updated != nil {
				t.Fatal("非法表单不应调用写服务")
			}
		}
	}
	assertDrawerHTML(t, post("/menus/update", menuForm), http.StatusOK)
	wantMenu := &admindto.MenuUpdateReq{ID: 42, Title: "修改菜单", Type: 2, Status: 0, PermissionCodes: []string{"menu:list", "menu:create"}, TitleKey: &titleKey, ExternalURL: "https://example.test/kept", IsHidden: 1, IsPublic: 1}
	if !reflect.DeepEqual(menus.updated, wantMenu) {
		t.Fatalf("菜单原生绑定或未编辑字段保留错误：%+v", menus.updated)
	}
	// 清空所有复选框是明确替换，不从 query 或旧值补回权限。
	menuForm.Del("permission_codes")
	assertDrawerHTML(t, post("/menus/update", menuForm), http.StatusOK)
	if len(menus.updated.PermissionCodes) != 0 {
		t.Fatalf("权限全取消应交空集合：%+v", menus.updated.PermissionCodes)
	}
	assertDrawerHTML(t, post("/permissions/update", permForm), http.StatusOK)
	if perms.updated.ID != 42 || perms.updated.PermissionName != "修改权限" || perms.updated.Status != 0 || perms.updated.Remark != "" {
		t.Fatalf("权限更新原生字段错误：%+v", perms.updated)
	}
	for _, path := range []string{"/menus/update", "/permissions/update"} {
		menus.updateErr, perms.updateErr = adminenums.Err(adminenums.ErrMenuCircle), adminenums.Err(adminenums.ErrCodeImmutable)
		form := menuForm
		if path == "/permissions/update" {
			form = permForm
		}
		assertDrawerHTML(t, post(path, form), http.StatusBadRequest)
		menus.updateErr, perms.updateErr = errors.New("SQLSTATE private"), errors.New("SQLSTATE private")
		assertDrawerHTML(t, post(path, form), http.StatusInternalServerError)
	}
}

func TestPermissionJSONUpdatePreservesDataProtocol(t *testing.T) {
	s := &drawerPermService{}
	h := &Handle{perm: s}
	r := drawerEngine()
	r.POST("/api/permission/update", h.PermUpdate)
	for _, status := range []int{0, 1, -1, 2} {
		input := admindto.PermUpdateReq{ID: 42, PermissionCode: "permission:demo", PermissionName: "修改权限", Module: "admin", APIPath: "/api/demo", APIMethod: "POST", Status: status}
		body, err := jsonx.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		s.updated = nil
		req := httptest.NewRequest(http.MethodPost, "/api/permission/update", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		want := http.StatusOK
		if status < 0 || status > 1 {
			want = http.StatusBadRequest
		}
		if w.Code != want || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("数据接口协议或状态范围错误：status=%d response=%d %s", status, w.Code, w.Body.String())
		}
		if want == http.StatusOK && (s.updated == nil || s.updated.Status != status) {
			t.Fatalf("合法状态应原样交服务：%+v", s.updated)
		}
		if want != http.StatusOK && s.updated != nil {
			t.Fatalf("非法状态不能调用写服务：%+v", s.updated)
		}
	}
}

func TestDrawerListOpenersRenderHTMLProtocol(t *testing.T) {
	for _, domain := range []string{"menus", "permissions"} {
		t.Run(domain, func(t *testing.T) {
			r := drawerEngine()
			r.GET("/", view.HTMLPage, func(c *gin.Context) {
				data := gin.H{
					"t": func(_ string, fallback string) string { return fallback }, "csrf_token": "test-csrf",
					"Buttons": map[string]bool{"menu.update": true, "permission.update": true}, "Total": int64(1),
					"FilterCode": "", "FilterModule": "", "Parents": []admindto.MenuParentChoice{}, "PermChoices": []permChoiceGroup{},
				}
				if domain == "menus" {
					data["Rows"] = []admindto.MenuDetailResp{{ID: 42, Title: "菜单", Type: 2}}
				} else {
					data["Rows"] = []admindto.PermDetailResp{{ID: 42, PermissionName: "权限"}}
				}
				c.HTML(http.StatusOK, "admin/system/"+domain+".html", data)
			})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
			body := w.Body.String()
			if w.Code != http.StatusOK || !strings.Contains(body, "<html") {
				t.Fatalf("列表真实模板渲染失败：%d %s", w.Code, body)
			}
			if strings.Count(body, `data-drawer-open hx-get="/admin/`+domain+`/edit?id=42" hx-target="#admin-drawer-body" hx-swap="innerHTML"`) != 2 {
				t.Fatalf("标题与操作列必须声明远程 HTML 抽屉：%s", body)
			}
			for _, retired := range []string{"data-drawer-url", "data-drawer-template", "data-bind", "tpl-menu-edit", "tpl-perm-edit"} {
				if strings.Contains(body, retired) {
					t.Fatalf("列表仍有旧 JSON 抽屉声明 %s", retired)
				}
			}
		})
	}
}

func TestDrawerRoutesDeclareHTMLBeforeAuthorization(t *testing.T) {
	r := drawerEngine()
	SetupAdminPages(r.Group("/admin"), nil, nil, &drawerPermService{}, &drawerMenuService{}, nil, nil, nil, nil)
	for _, path := range []string{"/admin/menus/edit", "/admin/menus/update", "/admin/permissions/edit", "/admin/permissions/update", "/admin/administrators/roles/drawer", "/admin/administrators/menus/drawer", "/admin/roles/permissions/drawer"} {
		method := http.MethodGet
		if strings.HasSuffix(path, "/update") {
			method = http.MethodPost
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Accept", "application/json")
		r.ServeHTTP(w, req)
		assertDrawerHTML(t, w, http.StatusUnauthorized)
	}
}
