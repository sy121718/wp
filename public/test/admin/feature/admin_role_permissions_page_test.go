package feature

// admin_role_permissions_page_test.go — 角色权限分配页（/admin/roles/permissions）的页面级用例。
//
// 覆盖两件在别处看不到的事：
//  1. 勾选态是**服务端渲染**的（禁用 JS 时页面也必须正确）—— 目录与菜单的 checkbox
//     都要 checked，因为「只勾按钮、没勾它所属的菜单」这个陷阱的补齐发生在服务端；
//  2. 表单解析：空值、非数字、重复 id 都不该让保存失败（真实落库集合由 service 侧
//     重新过滤，那里有真实 PostgreSQL 用例钉着）。
//
// 走真实链路：gin 路由 → AdminPagesHandle → shell.Prepare → Jet 模板 → 响应体。
// 角色服务用本包的假实现（admin_err_response_test.go 里的 fakeRoleService）。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	admindto "go_wp/internal/module/admin/dto"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/internal/templates"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
)

// newAdminRolePermissionsEngine 装配带真实 Jet 渲染的角色权限分配页路由。
func newAdminRolePermissionsEngine(t *testing.T, svc *fakeRoleService) *gin.Engine {
	t.Helper()
	handle := adminhttp.NewAdminPagesHandle(nil, svc, nil, nil, nil, nil)
	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		ConfigPath:     support.NewComponentTestConfig(t),
		GinMode:        gin.TestMode,
		InitComponents: true, // 让 shell.Prepare 拿到 i18n / 会话组件
		RouteRegistrar: func(e *gin.Engine) {
			e.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
			e.GET("/admin/roles/permissions", handle.RolePermissionsPage)
			e.POST("/admin/roles/permissions/save", handle.RolePermissionsSave)
		},
	})
	if err != nil {
		t.Fatalf("隔离依赖已就绪，组件初始化失败: %v", err)
	}
	t.Cleanup(func() { _ = cleanup() })
	return engine
}

// permTreeFixture 目录 → 菜单 → 按钮，外加一个未勾选的兄弟菜单（用来证明 checked 不是无差别输出）。
func permTreeFixture() *admindto.RolePermissionTreeResp {
	return &admindto.RolePermissionTreeResp{
		RoleID:   7,
		RoleCode: "editor",
		RoleName: "编辑",
		// 已补齐祖先：目录 1、菜单 2、按钮 3 全在集合里。
		MenuIDs: []uint64{1, 2, 3},
		Tree: []admindto.MenuTreeNode{
			{
				ID: 1, Title: "内容", Type: adminmodel.MenuTypeDirectory, Status: 1,
				Children: []admindto.MenuTreeNode{
					{
						ID: 2, ParentID: 1, Title: "文章", Type: adminmodel.MenuTypeMenu,
						Path: "/admin/articles", PermissionCode: "content:list", Status: 1,
						Children: []admindto.MenuTreeNode{
							{
								ID: 3, ParentID: 2, Title: "删除文章", Type: adminmodel.MenuTypeButton,
								PermissionCode: "content:delete", Status: 1,
							},
						},
					},
					{
						ID: 4, ParentID: 1, Title: "分类", Type: adminmodel.MenuTypeMenu,
						Path: "/admin/categories", PermissionCode: "content:category", Status: 1,
					},
				},
			},
		},
	}
}

// permCheckbox 取出页面上某个菜单 id 的勾选框整段 HTML。
//
// 不对整页做 strings.Contains("checked")：一个 checked 就能满足整页断言，
// 于是「未勾选的节点也被渲染成 checked」这种缺陷会漏过。
func permCheckbox(t *testing.T, body, id string) string {
	t.Helper()
	re := regexp.MustCompile(`<input[^>]*value="` + regexp.QuoteMeta(id) + `"[^>]*>`)
	got := re.FindString(body)
	if got == "" {
		t.Fatalf("页面里没有 value=%q 的勾选框", id)
	}
	return got
}

func fetchRolePermissionsPage(t *testing.T, engine *gin.Engine, path string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)
	return recorder.Code, recorder.Body.String()
}

// TestAdminRolePermissionsPageRendersCheckedNodes 服务端把「已补齐祖先」的勾选态渲染进 HTML。
func TestAdminRolePermissionsPageRendersCheckedNodes(t *testing.T) {
	engine := newAdminRolePermissionsEngine(t, &fakeRoleService{permTree: permTreeFixture()})

	code, body := fetchRolePermissionsPage(t, engine, "/admin/roles/permissions?role_id=7")
	if code != http.StatusOK {
		t.Fatalf("GET 页面状态码 %d", code)
	}

	// 目录（1）也必须在勾选态里：目录没有权限码，反查永远查不出它，
	// 这一条正是「目录也要勾选」在页面上的落点。
	for _, id := range []string{"1", "2", "3"} {
		if !strings.Contains(permCheckbox(t, body, id), "checked") {
			t.Fatalf("已授权节点 %s 应渲染为已勾选", id)
		}
	}
	// 未授权的兄弟菜单不得被渲染成勾选（否则上面那三条断言可以被一句「全都 checked」满足）。
	if strings.Contains(permCheckbox(t, body, "4"), "checked") {
		t.Fatal("未授权节点 4 不应渲染为已勾选")
	}

	// 层级与父子关系是 JS 联动与折叠的全部依据，必须是行上的数据而不是嵌套结构。
	if !strings.Contains(body, `data-perm-node data-id="3" data-parent="2"`) {
		t.Fatal("按钮节点缺少 data-parent（JS 无法向上补齐祖先）")
	}
	// 权限码要显示出来，配置者才能把「勾了什么」和「开了哪个接口」对上。
	if !strings.Contains(body, "content:delete") {
		t.Fatal("页面未显示节点的权限码")
	}
}

// TestAdminRolePermissionsPageRedirectsWithoutRoleID 缺少 role_id 时回列表并说明原因，不渲染半张空页。
//
// 出口形态收口后（见 admin_page_write_failed_test.go 文件头）：回跳地址带 `?err=`，
// 用户从书签/历史/截断参数进来时能看到「为什么被弹回去」，而不是静默落到角色列表。
func TestAdminRolePermissionsPageRedirectsWithoutRoleID(t *testing.T) {
	engine := newAdminRolePermissionsEngine(t, &fakeRoleService{})

	req := httptest.NewRequest(http.MethodGet, "/admin/roles/permissions", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("缺少 role_id 应 303 回列表，实际 %d", recorder.Code)
	}
	loc := recorder.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("回跳地址无法解析: %q", loc)
	}
	if u.Path != "/admin/roles" {
		t.Fatalf("跳转目标不符: %q（期望路径 /admin/roles）", loc)
	}
	// 静默重定向是本条要防的回归：文案缺失时运营完全不知道发生了什么。
	if errText := u.Query().Get("err"); errText == "" {
		t.Fatalf("缺少 role_id 的回跳必须带 ?err= 说明原因（否则是静默失败）：Location=%q", loc)
	}
}

// TestAdminRolePermissionsSaveParsesMenuIDs 表单里混着脏值时保存仍然成功，且不丢合法 id。
func TestAdminRolePermissionsSaveParsesMenuIDs(t *testing.T) {
	svc := &fakeRoleService{}
	engine := newAdminRolePermissionsEngine(t, svc)

	form := url.Values{}
	form.Set("role_id", "7")
	form.Add("menu_ids", "3")
	form.Add("menu_ids", "3")   // 重复：handler 不去重（service 侧 ListByIDs 天然去重）
	form.Add("menu_ids", "")    // 空值
	form.Add("menu_ids", "abc") // 非数字：解析成 0 后被丢弃
	form.Add("menu_ids", "12")

	req := httptest.NewRequest(http.MethodPost, "/admin/roles/permissions/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("保存应 303，实际 %d，体: %s", recorder.Code, recorder.Body.String())
	}
	// 回到分配页而不是列表页：这个页面的语义是「编辑一个集合」，
	// 保存后留在原地才能看见服务端重新渲染出的真实勾选态。
	if loc := recorder.Header().Get("Location"); loc != "/admin/roles/permissions?role_id=7" {
		t.Fatalf("保存后跳转目标不符: %q", loc)
	}
	if svc.lastMenuSave == nil {
		t.Fatal("没有调用服务层")
	}
	if svc.lastMenuSave.RoleID != 7 {
		t.Fatalf("role_id 传递不符: %d", svc.lastMenuSave.RoleID)
	}
	want := []uint64{3, 3, 12}
	got := svc.lastMenuSave.MenuIDs
	if len(got) != len(want) {
		t.Fatalf("合法 id 数量不符: got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("合法 id 不符: got=%v want=%v", got, want)
		}
	}
}

// TestAdminRolePermissionsSaveEmptySelectionIsSubmitted 一个都没勾也是合法提交（清空权限）。
//
// 这一条最容易在实现里写错：把「空集合」当成「没选，忽略本次提交」会让运营
// 永远清不掉一个多余角色上的权限，而且页面上没有任何异常。
func TestAdminRolePermissionsSaveEmptySelectionIsSubmitted(t *testing.T) {
	svc := &fakeRoleService{}
	engine := newAdminRolePermissionsEngine(t, svc)

	form := url.Values{}
	form.Set("role_id", "7")
	form.Set("menu_ids", "") // 浏览器在全部取消勾选时给不出任何 menu_ids

	req := httptest.NewRequest(http.MethodPost, "/admin/roles/permissions/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("空提交应 303，实际 %d", recorder.Code)
	}
	if svc.lastMenuSave == nil {
		t.Fatal("空提交也必须调用服务层（否则清空权限无从生效）")
	}
	if len(svc.lastMenuSave.MenuIDs) != 0 {
		t.Fatalf("空提交不应带出任何 id: %v", svc.lastMenuSave.MenuIDs)
	}
}

// TestAdminRolePermissionsSaveRejectsMissingRoleID 缺 role_id 时 303 回列表 + ?err=，不落库。
//
// 出口形态收口后（见 admin_page_write_failed_test.go 文件头）：页面写失败从 400 + JSON
// 改为 303 + `?err=`。**核心意图不变**：参数不合法时绝不调用服务层（不落库）。
// 回跳目标按有无 role_id 分流 —— 无 role_id 即没有可返回的分配页，只能回角色列表。
func TestAdminRolePermissionsSaveRejectsMissingRoleID(t *testing.T) {
	svc := &fakeRoleService{}
	engine := newAdminRolePermissionsEngine(t, svc)

	req := httptest.NewRequest(http.MethodPost, "/admin/roles/permissions/save", strings.NewReader("menu_ids=3"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("缺 role_id 应 303 回列表页，实际 %d（body=%s）", recorder.Code, recorder.Body.String())
	}
	loc := recorder.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("回跳地址无法解析: %q", loc)
	}
	if u.Path != "/admin/roles" {
		t.Fatalf("缺 role_id 应回角色列表（没有可返回的分配页），实际 %q", loc)
	}
	if errText := u.Query().Get("err"); errText == "" {
		t.Fatalf("回跳必须带 ?err= 说明原因（否则是静默失败）：Location=%q", loc)
	}
	if svc.lastMenuSave != nil {
		t.Fatal("缺 role_id 不应调用服务层")
	}
}
