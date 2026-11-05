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
	"go_wp/internal/text"
	jsonx "go_wp/pkg/jsonx"
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
			// 模板标题归模板本地取词（{{block pageTitle()}}），取词函数由请求级
			// 作用域注入 —— 与生产 assembly 的接线一致。缺这一步模板会在执行期
			// 拿不到 t，页面直接失败成空 body。
			e.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
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

// TestAdminRolesPageListLoadFailureIsJSON 覆盖「页面 GET 失败」的真实协议。
//
// 旧断言期望「降级渲染 200 的页面骨架 + 提示条」，那是失败页形状，已退役：
// 页面 GET 失败与其它失败一样只回 JSON {code,message,data} + 真实状态码，
// 不渲染半截页面（AGENTS.md「响应协议」）。
func TestAdminRolesPageListLoadFailureIsJSON(t *testing.T) {
	engine := newAdminRolesPageEngineWith(t, &fakeRoleService{err: errors.New(dbErrText)})
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/roles", nil))

	if recorder.Code < http.StatusInternalServerError {
		t.Fatalf("装载失败应至少 500，got %d body=%s", recorder.Code, recorder.Body.String())
	}
	if ct := recorder.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("页面 GET 失败也必须回 JSON，got Content-Type=%q body=%s", ct, recorder.Body.String())
	}
	body := recorder.Body.String()
	if strings.Contains(body, "<html") || strings.Contains(body, `name="keyword"`) {
		t.Fatalf("失败响应不得渲染页面骨架：%s", body)
	}

	var payload struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := jsonx.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("不是合法 JSON：%v %s", err, body)
	}
	if payload.Code == 0 || payload.Message == "" {
		t.Fatalf("失败响应缺 code/message：%s", body)
	}
	assertPageNoLeak(t, "角色列表装载失败", body)
}

// TestAdminRolesPageLoadFailureIgnoresLegacyErrQuery 守两件事：
// 装载失败时不再有页面提示条（没有失败页），且旧的 ?err= 回显通道依然关着。
func TestAdminRolesPageLoadFailureIgnoresLegacyErrQuery(t *testing.T) {
	engine := newAdminRolesPageEngineWith(t, &fakeRoleService{err: errors.New(dbErrText)})

	stale := "已删除 3 个角色，2 个未能删除（受保护或被引用）"
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/roles?err="+url.QueryEscape(stale), nil))

	if recorder.Code < http.StatusInternalServerError {
		t.Fatalf("装载失败应至少 500，got %d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if strings.Contains(body, stale) {
		t.Fatalf("旧 ?err= 的文案不得出现在响应里：%s", body)
	}
	if strings.Contains(body, `role="alert"`) {
		t.Fatalf("失败响应不再有页面提示条：%s", body)
	}
	assertPageNoLeak(t, "角色列表装载失败（带旧 ?err=）", body)
}
