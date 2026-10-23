package feature

// navigations_page_test.go — 导航菜单管理页渲染冒烟。
// 页面是纯服务端渲染（Jet 模板 + HTMX 表单），此处只验证「能渲染出菜单结构」，
// 写操作链路由 service 层用例覆盖。

import (
	"context"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/adminctx"
	navigationhttp "go_wp/internal/module/navigation/inbound/http"
	navsource "go_wp/internal/module/navigation/outbound/source"
	pagedto "go_wp/internal/module/page/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/permission"
	"go_wp/internal/templates"

	"github.com/gin-gonic/gin"
)

func renderNavigationsPage(t *testing.T, page gin.HandlerFunc, projectID string, perms map[string]bool) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.GET("/admin/navigations", adminui.PageSlot(navigationhttp.NavigationsPageTemplate), func(c *gin.Context) {
		c.Set(adminctx.PermSetKey, perms)
		c.Set(adminctx.ButtonsKey, buttonsOf(perms))
		page(c)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/navigations?project="+projectID+"&kind=header", nil)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("页面渲染失败，状态码 %d", recorder.Code)
	}
	return recorder.Body.String()
}

// TestNavigationsPageRenders 管理页渲染出菜单结构（含子项与操作入口）。
func TestNavigationsPageRenders(t *testing.T) {
	db, _, navSvc, projectID := newNavigationEnv(t)
	parentID := addNavItem(t, navSvc, projectID, "首页", "/", nil, "self")
	addNavItem(t, navSvc, projectID, "新品", "/new", &parentID, "blank")

	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	handle := navigationhttp.NewNavigationPageHandle(navSvc, projects)
	body := renderNavigationsPage(t, handle.NavigationsPage, projectID, map[string]bool{string(permission.NavigationUpdate): true})
	for _, want := range []string{"导航菜单", "首页", "新品", "/api/navigation/create"} {
		if !strings.Contains(body, want) {
			t.Errorf("页面缺少 %q", want)
		}
	}
	// 上移 / 下移也迁到真实 JSON 端点（action=move）：页面只剩声明，旧页面 POST 退役。
	// 放行标记挂在外层 span（基座放行 [data-requires-js] 时会摘 disabled，边界语义要留在按钮上）。
	for _, want := range []string{
		`data-write-url="/api/navigation/update"`,
		`data-write="create"`,
		`<input type="hidden" name="action" value="move">`,
		`name="dir" value="up"`,
		`name="dir" value="down"`,
		`<span data-requires-js hidden><button type="submit" form="nav-up-`,
		`<span data-requires-js hidden><button type="submit" form="nav-down-`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("排序表单缺少 JSON 写声明 %q", want)
		}
	}
	if strings.Contains(body, "/admin/navigations/move") {
		t.Error("页面 POST /admin/navigations/move 应已退役")
	}
	// 新建与删除已迁到真实 JSON 端点（页面 POST 退役）：页面上只剩端点声明，请求由基座发出。
	for _, want := range []string{
		`data-write-url="/api/navigation/create"`,
		`data-write-url="/api/navigation/delete"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("页面缺少 JSON 写端点声明 %q", want)
		}
	}
}

// TestNavigationsPageShowsSourceCandidates 管理页展示来源候选（按来源分组勾选添加）。
func TestNavigationsPageShowsSourceCandidates(t *testing.T) {
	db, pages, navSvc, projectID := newNavigationEnv(t)
	ctx := context.Background()
	if _, err := pages.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/about", DraftDocument: json.RawMessage(aboutDocument),
	}); err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	// 注入来源解析器（页面契约）：管理页据此列出候选。
	navSvc.SetSourceResolver(navsource.New(pages, nil, nil, nil))

	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	handle := navigationhttp.NewNavigationPageHandle(navSvc, projects)
	body := renderNavigationsPage(t, handle.NavigationsPage, projectID, map[string]bool{string(permission.NavigationUpdate): true})
	for _, want := range []string{"从已有内容添加", "/about", "页面（1）"} {
		if !strings.Contains(body, want) {
			t.Errorf("页面缺少 %q", want)
		}
	}
	// 来源批量加入已迁到真实 JSON 端点：抽屉表单只声明事实（端点 / 批量档 / outcome / 具名字段 /
	// 分支判别键 / 结果区），请求由基座发出 —— 页面 POST 与它的 action=query 回跳链一起退役。
	for _, want := range []string{
		`data-write-url="/api/navigation/create"`,
		`data-write="bulk"`,
		`data-write-outcome="add"`,
		`data-write-extra="action,projectId,kind,sourceType"`,
		`<input type="hidden" name="action" value="addSource">`,
		`name="ids"`,
		`data-write-status`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("来源抽屉缺少 JSON 写声明 %q", want)
		}
	}
	if strings.Contains(body, "/admin/navigations/add-source") {
		t.Error("页面 POST /admin/navigations/add-source 应已退役")
	}

	withoutUpdate := renderNavigationsPage(t, handle.NavigationsPage, projectID, map[string]bool{})
	if !strings.Contains(withoutUpdate, "导航菜单") {
		t.Error("无更新权限时页面主体未渲染")
	}
	for _, forbidden := range []string{
		`data-drawer-open="#tpl-nav-create"`, `id="tpl-nav-create"`, `data-write-url="/api/navigation/create"`,
		`data-drawer-open="#tpl-nav-add-source"`, `id="tpl-nav-add-source"`, `data-write-outcome="add"`,
	} {
		if strings.Contains(withoutUpdate, forbidden) {
			t.Errorf("无更新权限时页面仍出现操作 %q", forbidden)
		}
	}
}

// buttonsOf 把「权限码集合」转成「按钮码集合」（按钮码 = 权限码的 slug 形式，见迁移 589）。
// 测试直接给权限码更贴近业务语义；模板读的是按钮码，所以两条都注入。
func buttonsOf(perms map[string]bool) map[string]bool {
	out := make(map[string]bool, len(perms))
	for code := range perms {
		out[strings.ReplaceAll(code, ":", ".")] = true
	}
	return out
}
