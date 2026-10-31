package feature

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

	for _, want := range []string{
		`data-write-url="/api/navigation/create"`,
		`data-write-url="/api/navigation/delete"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("页面缺少 JSON 写端点声明 %q", want)
		}
	}
}

func TestNavigationsPageShowsSourceCandidates(t *testing.T) {
	db, pages, navSvc, projectID := newNavigationEnv(t)
	ctx := context.Background()
	if _, err := pages.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/about", DraftDocument: json.RawMessage(aboutDocument),
	}); err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}

	navSvc.SetSourceResolver(navsource.New(pages, nil, nil, nil))

	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	handle := navigationhttp.NewNavigationPageHandle(navSvc, projects)
	body := renderNavigationsPage(t, handle.NavigationsPage, projectID, map[string]bool{string(permission.NavigationUpdate): true})
	for _, want := range []string{"从已有内容添加", "/about", "页面（1）"} {
		if !strings.Contains(body, want) {
			t.Errorf("页面缺少 %q", want)
		}
	}

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

func buttonsOf(perms map[string]bool) map[string]bool {
	out := make(map[string]bool, len(perms))
	for code := range perms {
		out[strings.ReplaceAll(code, ":", ".")] = true
	}
	return out
}
