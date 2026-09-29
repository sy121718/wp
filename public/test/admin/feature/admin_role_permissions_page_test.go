package feature

// admin_role_permissions_page_test.go — 角色权限分配的抽屉片段
// （GET /admin/roles/permissions/drawer）与其保存出口的用例。
//
// 覆盖四件在别处看不到的事：
//  1. 片段形状符合 ui/drawer.js 的 fragmentRoot 校验（唯一根 + data-drawer-fragment + 含 form，
//     且**不含 <script>**）—— 形状不合的响应在浏览器里表现为「编辑表单加载失败，请重试」，
//     后端测试不钉这一条，就只能靠人肉点开抽屉才发现；
//  2. 勾选态是**服务端渲染**的（禁用 JS 时片段也必须正确）—— 目录与菜单的 checkbox
//     都要 checked，因为「只勾按钮、没勾它所属的菜单」这个陷阱的补齐发生在服务端；
//  3. 保存失败时回到片段里的是**本次提交的勾选**，不是库里的旧值（用户刚勾的那一屏不能丢）；
//  4. 表单解析：空值、非数字、重复 id 都不该让保存失败（真实落库集合由 service 侧
//     重新过滤，那里有真实 PostgreSQL 用例钉着）。
//
// 走真实链路：gin 路由 → AdminPagesHandle → shell.Prepare → Jet 模板 → 响应体。
// 角色服务用本包的假实现（admin_err_response_test.go 里的 fakeRoleService）。

import (
	"errors"
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

// 两个注入用的假错误：断言的是「原文不外泄」，所以它们必须是不可能出现在受控文案里的字符串。
var (
	errFakeRoleNotFound = errors.New("role permissions tree: relation does not exist")
	errFakeRoleSave     = errors.New("role menu save: duplicate key value violates unique constraint")
)

// newAdminRolePermissionsEngine 装配带真实 Jet 渲染的角色权限分配抽屉路由。
func newAdminRolePermissionsEngine(t *testing.T, svc *fakeRoleService) *gin.Engine {
	t.Helper()
	handle := adminhttp.NewAdminPagesHandle(nil, svc, nil, nil, nil, nil)
	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		ConfigPath:     support.NewComponentTestConfig(t),
		GinMode:        gin.TestMode,
		InitComponents: true, // 让 shell.Prepare 拿到 i18n / 会话组件
		RouteRegistrar: func(e *gin.Engine) {
			e.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
			e.GET("/admin/roles/permissions/drawer", handle.RolePermissionsDrawer)
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

// permCheckbox 取出片段里某个菜单 id 的勾选框整段 HTML。
//
// 不对整段做 strings.Contains("checked")：一个 checked 就能满足整段断言，
// 于是「未勾选的节点也被渲染成 checked」这种缺陷会漏过。
func permCheckbox(t *testing.T, body, id string) string {
	t.Helper()
	re := regexp.MustCompile(`<input[^>]*value="` + regexp.QuoteMeta(id) + `"[^>]*>`)
	got := re.FindString(body)
	if got == "" {
		t.Fatalf("片段里没有 value=%q 的勾选框", id)
	}
	return got
}

// assertDrawerFragment 钉住 ui/drawer.js 的 fragmentRoot 校验里最容易违反的两条：
// 唯一根（以 host 的 div 开头、以 </div> 结尾）与「片段里没有活动标记」。
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

// postRolePermissions 提交保存表单；hx 为真时带 HX-Request 头（抽屉里的真实路径）。
func postRolePermissions(t *testing.T, engine *gin.Engine, form url.Values, hx bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/admin/roles/permissions/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if hx {
		req.Header.Set("HX-Request", "true")
	}
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)
	return recorder
}

// TestAdminRolePermissionsDrawerRendersCheckedNodes 服务端把「已补齐祖先」的勾选态渲染进片段。
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

	// 目录（1）也必须在勾选态里：目录没有权限码，反查永远查不出它，
	// 这一条正是「目录也要勾选」在渲染上的落点。
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
		t.Fatal("片段未显示节点的权限码")
	}
	// 联动脚本住在基座（static/js/ui/perm-tree.js），片段只留锚点。
	if !strings.Contains(body, "data-perm-tree") {
		t.Fatal("权限树缺少 data-perm-tree 锚点（控件认领不到它）")
	}
}

// TestAdminRolePermissionsDrawerNoStore 片段不能被缓存：勾选态是「打开那一刻的策略快照」。
func TestAdminRolePermissionsDrawerNoStore(t *testing.T) {
	engine := newAdminRolePermissionsEngine(t, &fakeRoleService{permTree: permTreeFixture()})
	req := httptest.NewRequest(http.MethodGet, "/admin/roles/permissions/drawer?role_id=7", nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("片段必须 no-store，实际 %q", got)
	}
}

// TestAdminRolePermissionsDrawerRejectsMissingRoleID 缺少 role_id 时给状态码，不给半个片段。
//
// 抽屉对非 200 的响应会显示「编辑表单加载失败，请重试」并聚焦重试按钮 ——
// 服务端拼一个缺 form 的片段反而会被 fragmentRoot 判非法，用户看到的还是同一个失败界面。
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

// TestAdminRolePermissionsDrawerNotFound 角色不存在 / 树取数失败时 404，且响应里没有内部错误原文。
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

	recorder := postRolePermissions(t, engine, form, false)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("保存应 303，实际 %d，体: %s", recorder.Code, recorder.Body.String())
	}
	// 成功回角色列表：抽屉形态下没有可回的独立分配页。
	if loc := recorder.Header().Get("Location"); loc != "/admin/roles" {
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

	recorder := postRolePermissions(t, engine, form, false)

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
// 页面写失败从 400 + JSON 改为 303 + `?err=`（见 admin_page_write_failed_test.go 文件头）。
// **核心意图不变**：参数不合法时绝不调用服务层（不落库）。
func TestAdminRolePermissionsSaveRejectsMissingRoleID(t *testing.T) {
	svc := &fakeRoleService{}
	engine := newAdminRolePermissionsEngine(t, svc)

	recorder := postRolePermissions(t, engine, url.Values{"menu_ids": {"3"}}, false)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("缺 role_id 应 303 回列表页，实际 %d（body=%s）", recorder.Code, recorder.Body.String())
	}
	loc := recorder.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("回跳地址无法解析: %q", loc)
	}
	if u.Path != "/admin/roles" {
		t.Fatalf("缺 role_id 应回角色列表（没有可返回的抽屉），实际 %q", loc)
	}
	if errText := u.Query().Get("err"); errText == "" {
		t.Fatalf("回跳必须带 ?err= 说明原因（否则是静默失败）：Location=%q", loc)
	}
	if svc.lastMenuSave != nil {
		t.Fatal("缺 role_id 不应调用服务层")
	}
}

// TestAdminRolePermissionsSaveMissingRoleIDHtmxStillRedirects 缺 role_id 的 htmx 请求走 HX-Redirect。
//
// 抽屉里提交的请求都是 htmx 请求：此时附近没有可写的片段（缺 role_id 连树都取不了），
// 只能整页跳回列表 —— 用 HX-Redirect 而不是 302（XHR 会自己跟随 302，最终响应里读不到
// Location，整页 HTML 会被塞进片段的位置）。
func TestAdminRolePermissionsSaveMissingRoleIDHtmxStillRedirects(t *testing.T) {
	svc := &fakeRoleService{}
	engine := newAdminRolePermissionsEngine(t, svc)

	recorder := postRolePermissions(t, engine, url.Values{"menu_ids": {"3"}}, true)

	if recorder.Code != http.StatusOK {
		t.Fatalf("htmx 分支应 200 + HX-Redirect，实际 %d", recorder.Code)
	}
	loc := recorder.Header().Get("HX-Redirect")
	if !strings.HasPrefix(loc, "/admin/roles?err=") {
		t.Fatalf("htmx 回跳目标不符: %q", loc)
	}
	if svc.lastMenuSave != nil {
		t.Fatal("缺 role_id 不应调用服务层")
	}
}

// TestAdminRolePermissionsSaveHtmxFailureKeepsSelectionAndShape 失败时回片段自身：错误槽 + 提交的勾选。
//
// 两个断言各自防一类静默缺陷：
//
//	· 片段形状 —— 形状不合时 htmx 虽然会 swap 进去，但再下一次提交时抽屉里的节点已不是
//	  片段根，后续失败就无法再就地回报；
//	· **勾选来自本次提交**（节点 4 库里没勾、这次勾上了）—— 用库里的值渲回去会把用户
//	  刚勾的那一屏整块抹掉，而他正需要在这里改掉那个错误重试。
func TestAdminRolePermissionsSaveHtmxFailureKeepsSelectionAndShape(t *testing.T) {
	svc := &fakeRoleService{permTree: permTreeFixture(), menuSaveErr: errFakeRoleSave}
	engine := newAdminRolePermissionsEngine(t, svc)

	form := url.Values{}
	form.Set("role_id", "7")
	for _, id := range []string{"1", "2", "3", "4"} {
		form.Add("menu_ids", id)
	}

	recorder := postRolePermissions(t, engine, form, true)
	if recorder.Code != http.StatusOK {
		t.Fatalf("htmx 失败应 200（5xx 会被 htmx 判成不 swap，用户什么都看不到），实际 %d", recorder.Code)
	}
	if recorder.Header().Get("HX-Redirect") != "" {
		t.Fatal("树可取回来时必须就地回报，不能整页跳走")
	}
	body := recorder.Body.String()
	assertDrawerFragment(t, body)
	if !strings.Contains(body, `role="alert"`) {
		t.Fatal("失败片段缺少错误槽（role=alert）")
	}
	if strings.Contains(body, errFakeRoleSave.Error()) {
		t.Fatalf("失败片段泄漏了内部错误原文：%s", body)
	}
	if !strings.Contains(permCheckbox(t, body, "4"), "checked") {
		t.Fatal("失败回片段时勾选必须来自本次提交（节点 4 是这次才勾上的）")
	}
}

// TestAdminRolePermissionsSaveHtmxFailureFallsBackToList 连树都取不回来时整页回列表。
//
// 只剩空树可渲的片段会退化成「没有可分配的菜单」的空态 —— 那比一句通用错误更误导
// （看起来像这个角色的权限被清空了）。
func TestAdminRolePermissionsSaveHtmxFailureFallsBackToList(t *testing.T) {
	svc := &fakeRoleService{err: errFakeRoleSave}
	engine := newAdminRolePermissionsEngine(t, svc)

	form := url.Values{"role_id": {"7"}, "menu_ids": {"3"}}
	recorder := postRolePermissions(t, engine, form, true)

	if recorder.Code != http.StatusOK {
		t.Fatalf("htmx 分支应 200 + HX-Redirect，实际 %d", recorder.Code)
	}
	loc := recorder.Header().Get("HX-Redirect")
	if !strings.HasPrefix(loc, "/admin/roles?err=") {
		t.Fatalf("htmx 回跳目标不符: %q", loc)
	}
	if strings.Contains(recorder.Body.String(), "没有可分配的菜单") {
		t.Fatal("树取不回来时不能渲染空态片段（会被读成「权限被清空了」）")
	}
}

// TestAdminRolePermissionsSaveHtmxSuccessShowsReceipt 成功后抽屉里就地给回执，不整页跳走。
func TestAdminRolePermissionsSaveHtmxSuccessShowsReceipt(t *testing.T) {
	svc := &fakeRoleService{permTree: permTreeFixture()}
	engine := newAdminRolePermissionsEngine(t, svc)

	form := url.Values{}
	form.Set("role_id", "7")
	for _, id := range []string{"1", "2", "3"} {
		form.Add("menu_ids", id)
	}
	recorder := postRolePermissions(t, engine, form, true)

	if recorder.Code != http.StatusOK {
		t.Fatalf("htmx 成功应 200 + 成功态片段，实际 %d", recorder.Code)
	}
	if recorder.Header().Get("HX-Redirect") != "" {
		t.Fatal("成功不该整页跳走：列表页的角色行没有任何一列体现权限集合，没有可刷的新数据")
	}
	body := recorder.Body.String()
	assertDrawerFragment(t, body)
	if !strings.Contains(body, "权限已保存") {
		t.Fatalf("成功态缺少回执文案（否则用户关掉抽屉后无从判断是否生效）：%s", body)
	}
	if !strings.Contains(body, "data-drawer-close") {
		t.Fatal("成功态必须给一条关闭抽屉的出口")
	}
	if strings.Contains(body, `name="menu_ids"`) {
		t.Fatal("成功态不该再渲染权限树（勾选已提交，回执里那棵树会被误读成本次保存的结果快照）")
	}
}
