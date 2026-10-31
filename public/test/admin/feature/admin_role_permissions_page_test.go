package feature

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go_wp/internal/adminui"
	admindto "go_wp/internal/module/admin/dto"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/internal/templates"
	"go_wp/pkg/jsonx"
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
			e.GET("/admin/roles/permissions/drawer", adminui.BareSlot(adminhttp.RolePermissionsFragmentTemplate), handle.RolePermissionsDrawer)
			e.POST("/api/role/menu/save", func(c *gin.Context) {
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
		RoleID:   7,
		RoleCode: "editor",
		RoleName: "编辑",

		MenuIDs: []uint64{1, 2, 3},
		Tree: []admindto.MenuTreeNode{
			{
				ID: 1, Title: "内容", Type: adminmodel.MenuTypeDirectory, Status: 1,
				Children: []admindto.MenuTreeNode{
					{
						ID: 2, ParentID: 1, Title: "文章", Type: adminmodel.MenuTypeMenu,
						Path: "/admin/articles", PermissionCodes: []string{"content:list"}, Status: 1,
						Children: []admindto.MenuTreeNode{
							{
								ID: 3, ParentID: 2, Title: "删除文章", Type: adminmodel.MenuTypeButton,
								PermissionCodes: []string{"content:delete"}, Status: 1,
							},
						},
					},
					{
						ID: 4, ParentID: 1, Title: "分类", Type: adminmodel.MenuTypeMenu,
						Path: "/admin/categories", PermissionCodes: []string{"content:category"}, Status: 1,
					},
				},
			},
		},
	}
}

func permCheckbox(t *testing.T, body, id string) string {
	t.Helper()
	re := regexp.MustCompile(`<input[^>]*value="` + regexp.QuoteMeta(id) + `"[^>]*>`)
	got := re.FindString(body)
	if got == "" {
		t.Fatalf("片段里没有 value=%q 的勾选框", id)
	}
	return got
}

func assertDrawerFragment(t *testing.T, body string) {
	t.Helper()
	trimmed := strings.TrimSpace(body)
	if !strings.HasPrefix(trimmed, `<div data-drawer-fragment`) || !strings.HasSuffix(trimmed, `</div>`) {
		t.Fatalf("片段必须是唯一的 data-drawer-fragment 根元素：%s", trimmed)
	}
	for _, banned := range []string{"<script", "<style", "<template", "<svg", "<link"} {
		if strings.Contains(trimmed, banned) {
			t.Fatalf("片段里出现了 %s：drawer.js 的 fragmentRoot 会整段拒收（表现为「加载失败，请重试」）", banned)
		}
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
	if code != http.StatusOK {
		t.Fatalf("GET 片段状态码 %d", code)
	}
	assertDrawerFragment(t, body)
	if !strings.Contains(body, "<form") {
		t.Fatal("片段必须自带表单（drawer.js 的 fragmentRoot 要求）")
	}

	for _, attr := range []string{`action="/api/role/menu/save"`, `data-write="update"`, `data-write-url="/api/role/menu/save"`, `data-write-status`, `data-drawer-close`} {
		if !strings.Contains(body, attr) {
			t.Fatalf("权限表单缺少既有JSON写基座声明 %s", attr)
		}
	}
	if strings.Contains(body, "/admin/roles/permissions/save") {
		t.Fatal("权限表单仍调用旧HTML写出口")
	}

	for _, id := range []string{"1", "2", "3"} {
		if !strings.Contains(permCheckbox(t, body, id), "checked") {
			t.Fatalf("已授权节点 %s 应渲染为已勾选", id)
		}
	}

	if strings.Contains(permCheckbox(t, body, "4"), "checked") {
		t.Fatal("未授权节点 4 不应渲染为已勾选")
	}

	if !strings.Contains(body, `data-perm-node data-id="3" data-parent="2"`) {
		t.Fatal("按钮节点缺少 data-parent（JS 无法向上补齐祖先）")
	}

	if !strings.Contains(body, "content:delete") {
		t.Fatal("片段未显示节点的权限码")
	}

	if !strings.Contains(body, "data-perm-tree") {
		t.Fatal("权限树缺少 data-perm-tree 锚点（控件认领不到它）")
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
	if code != http.StatusBadRequest {
		t.Fatalf("缺 role_id 应 400，实际 %d（body=%s）", code, body)
	}
	if strings.TrimSpace(body) != "" {
		t.Fatalf("失败响应不应带正文（内部细节不外泄）：%s", body)
	}
}

func TestAdminRolePermissionsDrawerNotFound(t *testing.T) {
	engine := newAdminRolePermissionsEngine(t, &fakeRoleService{err: errFakeRoleNotFound})

	code, body := fetchRolePermissionsDrawer(t, engine, "/admin/roles/permissions/drawer?role_id=7")
	if code != http.StatusNotFound {
		t.Fatalf("树取数失败应 404，实际 %d", code)
	}
	if strings.Contains(body, errFakeRoleNotFound.Error()) {
		t.Fatalf("响应泄漏了内部错误原文：%s", body)
	}
}

func assertRolePermissionsJSON(t *testing.T, rec *httptest.ResponseRecorder, status int) string {
	t.Helper()
	if rec.Code != status || !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("期望JSON %d，实际 %d: %s", status, rec.Code, rec.Body.String())
	}
	var payload struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := jsonx.Unmarshal(rec.Body.Bytes(), &payload); err != nil || payload.Code != status || payload.Message == "" {
		t.Fatalf("JSON状态或文案不符: %s (%v)", rec.Body.String(), err)
	}
	if rec.Header().Get("Location") != "" || rec.Header().Get("HX-Redirect") != "" {
		t.Fatal("API写响应不应导航")
	}
	for _, raw := range []string{"<form", "data-jump-state", "duplicate key", "relation does not exist", "SQLSTATE"} {
		if strings.Contains(rec.Body.String(), raw) {
			t.Fatalf("响应包含HTML或内部错误 %q: %s", raw, rec.Body.String())
		}
	}
	return payload.Message
}

func TestAdminRolePermissionsSaveParsesMenuIDs(t *testing.T) {
	svc := &fakeRoleService{}
	engine := newAdminRolePermissionsEngine(t, svc)
	rec := postRolePermissions(t, engine, url.Values{"role_id": {"7"}, "menu_ids": {"3", "3", "12"}, "operator_id": {"999"}}, false)
	assertRolePermissionsJSON(t, rec, http.StatusOK)
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
	assertRolePermissionsJSON(t, postRolePermissions(t, engine, url.Values{"role_id": {"7"}}, false), http.StatusOK)
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
				assertRolePermissionsJSON(t, postRolePermissions(t, engine, tt.form, hx), http.StatusBadRequest)
			}
			if svc.lastMenuSave != nil {
				t.Fatal("非法输入不得调用服务")
			}
		})
	}
}

func TestAdminRolePermissionsSaveFailureIsJSON(t *testing.T) {
	svc := &fakeRoleService{err: errFakeRoleSave}
	engine := newAdminRolePermissionsEngine(t, svc)
	form := url.Values{"role_id": {"7"}, "menu_ids": {"1", "2", "3", "4"}}
	assertRolePermissionsJSON(t, postRolePermissions(t, engine, form, true), http.StatusInternalServerError)
	if svc.lastMenuSave == nil || !slices.Equal(svc.lastMenuSave.MenuIDs, []uint64{1, 2, 3, 4}) {
		t.Fatalf("失败请求仍须交完整勾选: %+v", svc.lastMenuSave)
	}

}

func TestAdminRolePermissionsSaveJSONMatchesForm(t *testing.T) {
	svc := &fakeRoleService{}
	engine := newAdminRolePermissionsEngine(t, svc)
	form := url.Values{"role_id": {"7"}, "menu_ids": {"1", "2", "3"}}
	formMessage := assertRolePermissionsJSON(t, postRolePermissions(t, engine, form, false), http.StatusOK)
	formReq := *svc.lastMenuSave
	req := httptest.NewRequest(http.MethodPost, "/api/role/menu/save", strings.NewReader(`{"role_id":7,"menu_ids":[1,2,3],"operator_id":999}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if message := assertRolePermissionsJSON(t, rec, http.StatusOK); message != formMessage {
		t.Fatalf("JSON和表单成功文案不一致: %q != %q", message, formMessage)
	}
	got := svc.lastMenuSave
	if got.RoleID != formReq.RoleID || got.OperatorID != formReq.OperatorID || !slices.Equal(got.MenuIDs, formReq.MenuIDs) {
		t.Fatalf("JSON/表单效果不等: %+v != %+v", got, formReq)
	}
}

func TestAdminRolePermissionsSaveRequiresSessionIdentity(t *testing.T) {
	svc := &fakeRoleService{}
	engine := newAdminRolePermissionsEngineWithUser(t, svc, 0)
	assertRolePermissionsJSON(t, postRolePermissions(t, engine, url.Values{"role_id": {"7"}, "operator_id": {"999"}}, false), http.StatusUnauthorized)
	if svc.lastMenuSave != nil {
		t.Fatal("未登录不得调用服务")
	}
}
