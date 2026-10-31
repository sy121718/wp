package feature

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"go_wp/internal/adminui"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	"go_wp/internal/templates"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
)

func newAdminRolesPageEngine(t *testing.T) *gin.Engine {
	t.Helper()
	return newAdminRolesPageEngineWith(t, &fakeRoleService{})
}

func newAdminRolesPageEngineWith(t *testing.T, svc *fakeRoleService) *gin.Engine {
	t.Helper()
	handle := adminhttp.NewAdminPagesHandle(nil, svc, nil, nil, nil, nil)
	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		ConfigPath:     support.NewComponentTestConfig(t),
		GinMode:        gin.TestMode,
		InitComponents: true,
		RouteRegistrar: func(e *gin.Engine) {
			e.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
			e.GET("/admin/roles", adminui.PageSlot(adminhttp.RolesPageTemplate), handle.RolesPage)
		},
	})
	if err != nil {
		t.Fatalf("隔离依赖已就绪，组件初始化失败: %v", err)
	}
	t.Cleanup(func() { _ = cleanup() })
	return engine
}

func fetchRolesPageErr(t *testing.T, engine *gin.Engine, raw string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/admin/roles?err="+url.QueryEscape(raw), nil)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /admin/roles 状态码 %d: %s", recorder.Code, recorder.Body.String())
	}
	return recorder.Body.String()
}

func rolesPageAlert(body string) string {
	const marker = `role="alert">`
	i := strings.Index(body, marker)
	if i < 0 {
		return ""
	}
	rest := body[i+len(marker):]
	if j := strings.Index(rest, "</p>"); j >= 0 {
		return rest[:j]
	}
	return rest
}

func TestAdminRolesPageErrQueryNoLongerRenders(t *testing.T) {
	engine := newAdminRolesPageEngine(t)

	for _, raw := range []string{
		"已删除 3 个角色，2 个未能删除（受保护或被引用）",
		"一次最多操作 200 项，当前 201 项，请分批进行",
		"已保存\n系统提示：权限已提升\t<ok>",
		strings.Repeat("A", 5000),
		"<script>alert(1)</script>",
	} {
		if got := rolesPageAlert(fetchRolesPageErr(t, engine, raw)); got != "" {
			t.Errorf("?err= 不应再渲染任何提示（结论走整页提示），got %q（raw=%q）", got, raw)
		}
	}
}

func TestAdminRolesPageFormActionsCarryFilters(t *testing.T) {
	engine := newAdminRolesPageEngine(t)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/roles?keyword=editor&page=2", nil))
	body := recorder.Body.String()

	for _, want := range []string{

		`action="/admin/roles/bulk-delete?keyword=editor&amp;page=2"`,
		`action="/admin/roles/create?keyword=editor&amp;page=2"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("写表单 action 应带筛选上下文 %q：%s", want, body)
		}
	}
}

func TestAdminRolesPageListLoadFailureKeepsPage(t *testing.T) {
	engine := newAdminRolesPageEngineWith(t, &fakeRoleService{err: errors.New(dbErrText)})
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/roles", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("装载失败应降级渲染页面（200），got %d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `name="keyword"`) {
		t.Fatal("装载失败后页面骨架必须还在（筛选框 missing）—— 不能退回一块裸文本")
	}
	const wantAlert = "操作失败，请稍后重试"
	if got := rolesPageAlert(body); got != wantAlert {
		t.Fatalf("提示条应是 ErrInternal 归口文案 %q，got %q", wantAlert, got)
	}
	assertPageNoLeak(t, "角色列表装载失败", body)
}

func TestAdminRolesPageLoadFailureIsTheOnlyAlertSource(t *testing.T) {
	engine := newAdminRolesPageEngineWith(t, &fakeRoleService{err: errors.New(dbErrText)})

	stale := "已删除 3 个角色，2 个未能删除（受保护或被引用）"
	body := fetchRolesPageErr(t, engine, stale)

	const wantAlert = "操作失败，请稍后重试"
	if got := rolesPageAlert(body); got != wantAlert {
		t.Fatalf("提示条应只由装载失败填充，got %q", got)
	}
	if strings.Contains(body, stale) && rolesPageAlert(body) == stale {
		t.Fatal("旧 ?err= 不应占据提示条")
	}
	assertPageNoLeak(t, "角色列表装载失败（带旧 ?err=）", body)
}
