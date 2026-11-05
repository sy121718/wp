package feature

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"go_wp/internal/adminui"
	admindto "go_wp/internal/module/admin/dto"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/internal/templates"
	"go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
)

var (
	errFakeRoleNotFound = errors.New("role permissions tree: relation does not exist")
	errFakeRoleSave     = errors.New("role menu save: duplicate key value violates unique constraint")
)

func newAdminRolePermissionsEngine(t *testing.T, svc *fakeRoleService) *gin.Engine {
	t.Helper()
	return newAdminRolePermissionsEngineWithUser(t, svc, 123)
}

func newAdminRolePermissionsEngineWithUser(t *testing.T, svc *fakeRoleService, userID uint64) *gin.Engine {
	t.Helper()
	handle := adminhttp.NewAdminPagesHandle(nil, svc, nil, nil, nil, nil)
	api := adminhttp.NewHandleWithDeps(nil, svc, nil, nil, nil, nil)
	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		ConfigPath:     support.NewComponentTestConfig(t),
		GinMode:        gin.TestMode,
		InitComponents: true,
		RouteRegistrar: func(e *gin.Engine) {
			e.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
			e.Use(view.FailureMiddleware(adminui.RenderFailure))
			e.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) { view.FailScene(c, "template", err) }))
			e.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
			e.GET("/admin/roles/permissions/drawer", view.HTMLFragment, handle.RolePermissionsDrawer)
			e.POST("/api/role/menu/save", view.HTMLFragment, func(c *gin.Context) {
				c.Set("user_id", int64(userID))
			}, api.RoleMenuSave)
		},
	})
	if err != nil {
		t.Fatalf("隔离依赖已就绪，组件初始化失败: %v", err)
	}
	t.Cleanup(func() { _ = cleanup() })
	return engine
}

func permTreeFixture() *admindto.RolePermissionTreeResp {
	return &admindto.RolePermissionTreeResp{
		RoleID: 7, RoleCode: "editor", RoleName: "编辑",
		MenuIDs: []uint64{1, 2, 3},
		Tree: []admindto.MenuTreeNode{
			{
				ID: 1, Title: "内容", Type: adminmodel.MenuTypeDirectory, Status: 1,
				Children: []admindto.MenuTreeNode{
					{
						ID: 2, ParentID: 1, Title: "文章", Type: adminmodel.MenuTypeMenu,
						Path: "/admin/articles", PermissionCodes: []string{"content:list"}, Status: 1,
						Children: []admindto.MenuTreeNode{
							{ID: 3, ParentID: 2, Title: "删除文章", Type: adminmodel.MenuTypeButton, PermissionCodes: []string{"content:delete"}, Status: 1},
						},
					},
					{ID: 4, ParentID: 1, Title: "分类", Type: adminmodel.MenuTypeMenu, Path: "/admin/categories", PermissionCodes: []string{"content:category"}, Status: 1},
				},
			},
		},
	}
}

func fetchRolePermissionsDrawer(t *testing.T, engine *gin.Engine, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)
	return recorder.Code, recorder.Body.String()
}

func postRolePermissions(t *testing.T, engine *gin.Engine, form url.Values, hx bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/role/menu/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if hx {
		req.Header.Set("HX-Request", "true")
	}
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)
	return recorder
}

func TestAdminRolePermissionsDrawerRendersCheckedNodes(t *testing.T) {
	engine := newAdminRolePermissionsEngine(t, &fakeRoleService{permTree: permTreeFixture()})
	code, body := fetchRolePermissionsDrawer(t, engine, "/admin/roles/permissions/drawer?role_id=7")
	if code != http.StatusOK || !strings.Contains(body, `data-drawer-fragment`) || strings.Count(body, `data-perm-node`) != 4 {
		t.Fatalf("Jet 权限树错误: status=%d body=%s", code, body)
	}
	for _, id := range []uint64{1, 2, 3, 4} {
		if got := drawerCheckboxChecked(t, body, "menu_ids", itoa(id)); got != (id <= 3) {
			t.Fatalf("节点 %d 勾选错误: %v", id, got)
		}
	}
	for _, want := range []string{`data-id="3" data-parent="2" data-depth="3"`, "content:delete", `padding-left:50px`} {
		if !strings.Contains(body, want) {
			t.Fatalf("权限树缺少父级、权限或缩进 %q: %s", want, body)
		}
	}
}

func TestAdminRolePermissionsDrawerNoStore(t *testing.T) {
	engine := newAdminRolePermissionsEngine(t, &fakeRoleService{permTree: permTreeFixture()})
	req := httptest.NewRequest(http.MethodGet, "/admin/roles/permissions/drawer?role_id=7", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("片段必须 no-store，实际 %q", got)
	}
}

func TestAdminRolePermissionsDrawerRejectsMissingRoleID(t *testing.T) {
	engine := newAdminRolePermissionsEngine(t, &fakeRoleService{permTree: permTreeFixture()})
	code, body := fetchRolePermissionsDrawer(t, engine, "/admin/roles/permissions/drawer")
	if code != http.StatusBadRequest || !strings.Contains(body, "data-ui-error") || strings.Contains(body, "<form") {
		t.Fatalf("参数错误须返回 400 Jet 错误片段: %d %s", code, body)
	}
}

func TestAdminRolePermissionsDrawerInfrastructureFailure(t *testing.T) {
	engine := newAdminRolePermissionsEngine(t, &fakeRoleService{err: errFakeRoleNotFound})
	code, body := fetchRolePermissionsDrawer(t, engine, "/admin/roles/permissions/drawer?role_id=7")
	if code != http.StatusInternalServerError || !strings.Contains(body, "data-ui-error") || strings.Contains(body, errFakeRoleNotFound.Error()) {
		t.Fatalf("基础设施错误须脱敏且返回 HTML 500: %d %s", code, body)
	}
}

func assertRolePermissionsHTML(t *testing.T, rec *httptest.ResponseRecorder, status int) string {
	t.Helper()
	body := rec.Body.String()
	if rec.Code != status || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("期望 HTML %d，实际 %d: %s", status, rec.Code, body)
	}
	if rec.Header().Get("Location") != "" || rec.Header().Get("HX-Redirect") != "" {
		t.Fatal("写响应不应导航")
	}
	for _, raw := range []string{"<form", "<html", "data-jump-state", "duplicate key", "relation does not exist", "SQLSTATE"} {
		if strings.Contains(body, raw) {
			t.Fatalf("响应包含表单、整页或内部错误 %q: %s", raw, body)
		}
	}
	marker := `role="status"`
	if status >= 400 {
		marker = "data-ui-error"
	}
	if !strings.Contains(body, marker) || (status >= 400 && strings.Contains(body, `role="status"`)) {
		t.Fatalf("HTML 结果标记不符: %s", body)
	}
	return body
}

func TestAdminRolePermissionsSaveParsesMenuIDs(t *testing.T) {
	svc := &fakeRoleService{}
	engine := newAdminRolePermissionsEngine(t, svc)
	rec := postRolePermissions(t, engine, url.Values{"role_id": {"7"}, "menu_ids": {"3", "3", "12"}, "operator_id": {"999"}, "OperatorID": {"999"}}, false)
	assertRolePermissionsHTML(t, rec, http.StatusOK)
	if svc.lastMenuSave == nil || svc.lastMenuSave.RoleID != 7 || !slices.Equal(svc.lastMenuSave.MenuIDs, []uint64{3, 3, 12}) {
		t.Fatalf("输入未原样交服务: %+v", svc.lastMenuSave)
	}
	if svc.lastMenuSave.OperatorID != 123 {
		t.Fatalf("操作者必须来自会话: %+v", svc.lastMenuSave)
	}
}

func TestAdminRolePermissionsSaveEmptySelectionIsSubmitted(t *testing.T) {
	svc := &fakeRoleService{}
	engine := newAdminRolePermissionsEngine(t, svc)
	assertRolePermissionsHTML(t, postRolePermissions(t, engine, url.Values{"role_id": {"7"}}, false), http.StatusOK)
	if svc.lastMenuSave == nil || len(svc.lastMenuSave.MenuIDs) != 0 {
		t.Fatalf("空勾选必须交服务清空: %+v", svc.lastMenuSave)
	}
}

func TestAdminRolePermissionsSaveRejectsInvalidInput(t *testing.T) {
	for _, tt := range []struct {
		name string
		form url.Values
	}{
		{"缺角色", url.Values{"menu_ids": {"3"}}},
		{"非数字菜单", url.Values{"role_id": {"7"}, "menu_ids": {"abc"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeRoleService{}
			engine := newAdminRolePermissionsEngine(t, svc)
			for _, hx := range []bool{false, true} {
				assertRolePermissionsHTML(t, postRolePermissions(t, engine, tt.form, hx), http.StatusBadRequest)
			}
			if svc.lastMenuSave != nil {
				t.Fatal("非法输入不得调用服务")
			}
		})
	}
}

func TestAdminRolePermissionsSaveFailureIsHTML(t *testing.T) {
	svc := &fakeRoleService{err: errFakeRoleSave}
	engine := newAdminRolePermissionsEngine(t, svc)
	form := url.Values{"role_id": {"7"}, "menu_ids": {"1", "2", "3", "4"}}
	assertRolePermissionsHTML(t, postRolePermissions(t, engine, form, true), http.StatusInternalServerError)
	if svc.lastMenuSave == nil || !slices.Equal(svc.lastMenuSave.MenuIDs, []uint64{1, 2, 3, 4}) {
		t.Fatalf("失败请求仍须交完整勾选: %+v", svc.lastMenuSave)
	}
}

func TestAdminRolePermissionsSaveRejectsJSONAndQueryFallback(t *testing.T) {
	svc := &fakeRoleService{}
	engine := newAdminRolePermissionsEngine(t, svc)
	for _, input := range []struct{ contentType, body string }{
		{"application/json", `{"role_id":7,"menu_ids":[1,2,3],"operator_id":999}`},
		{"application/x-www-form-urlencoded", "menu_ids=3&OperatorID=999"},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/role/menu/save?role_id=7", strings.NewReader(input.body))
		req.Header.Set("Content-Type", input.contentType)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		assertRolePermissionsHTML(t, rec, http.StatusBadRequest)
	}
	if svc.lastMenuSave != nil {
		t.Fatal("不能从 JSON 或 query 补全表单参数")
	}
}

func TestAdminRolePermissionsSaveRequiresSessionIdentity(t *testing.T) {
	svc := &fakeRoleService{}
	engine := newAdminRolePermissionsEngineWithUser(t, svc, 0)
	assertRolePermissionsHTML(t, postRolePermissions(t, engine, url.Values{"role_id": {"7"}, "operator_id": {"999"}, "OperatorID": {"999"}}, false), http.StatusUnauthorized)
	if svc.lastMenuSave != nil {
		t.Fatal("未登录不得调用服务")
	}
}
