package feature

// navigations_page_test.go — 导航菜单管理页渲染冒烟。
// 页面是纯服务端渲染（Jet 模板 + HTMX 表单），此处只验证「能渲染出菜单结构」，
// 写操作链路由 service 层用例覆盖。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dashboardhttp "go_wp/internal/module/dashboard/inbound/http"
	navsource "go_wp/internal/module/navigation/outbound/source"
	pagedto "go_wp/internal/module/page/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"

	"github.com/gin-gonic/gin"
)

// TestNavigationsPageRenders 管理页渲染出菜单结构（含子项与操作入口）。
func TestNavigationsPageRenders(t *testing.T) {
	db, _, navSvc, projectID := newNavigationEnv(t)
	parentID := addNavItem(t, navSvc, projectID, "首页", "/", nil, "self")
	addNavItem(t, navSvc, projectID, "新品", "/new", &parentID, "blank")

	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	handle := dashboardhttp.NewHandle(nil, projects, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, navSvc)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.GET("/admin/navigations", handle.NavigationsPage)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/navigations?project="+projectID+"&kind=header", nil)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("页面渲染失败，状态码 %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, want := range []string{"导航菜单", "首页", "新品", "/admin/navigations/create", "/admin/navigations/move"} {
		if !strings.Contains(body, want) {
			t.Errorf("页面缺少 %q", want)
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
	handle := dashboardhttp.NewHandle(nil, projects, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, navSvc)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	router.GET("/admin/navigations", handle.NavigationsPage)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/navigations?project="+projectID+"&kind=header", nil)
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("页面渲染失败，状态码 %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, want := range []string{"从已有内容添加", "/admin/navigations/add-source", "/about", "页面（1）"} {
		if !strings.Contains(body, want) {
			t.Errorf("页面缺少 %q", want)
		}
	}
}
