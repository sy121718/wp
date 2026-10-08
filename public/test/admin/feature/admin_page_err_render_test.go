package feature

// admin_page_err_render_test.go — 列表页提示条的**唯一来源**与「?err= 不再进页面」的回归。
//
// 历史：这里曾钉住「?err= 读回来的任意串能不能渲染」——第四批起判据升级为**白名单整体匹配**
//（只有写侧真实产出过的文案才渲染，其余一律空串），因为查询参数不是可信边界：
//
//	GET /admin/roles?err=任意文本
//
// 本批把写动作的结论改成由 shell.RenderJump 渲染整页提示（见 internal/shell/jump.go）
// 之后，查询参数与页面提示条之间**不再有数据通路**：`?err=` 连一条空白提示都造不出来。
// 所以本文件的两条判据变成：
//  1. `?err=` 的任何取值都不渲染提示条（本文件上半）；
//  2. 列表页提示条只剩一个来源 —— **列表取数失败**（归口文案 + 完整页壳），下半。
//
// 走真实链路：gin 路由 → RolesPage → shell.Prepare → Jet 模板 → 响应体。
// 角色服务用同包的假实现（返回空列表），本用例只关心 Err 这一个模板键。

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	adminhttp "go_wp/internal/module/admin/inbound/http"
	"go_wp/internal/templates"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
)

// newAdminRolesPageEngine 装配带真实 Jet 渲染的 /admin/roles（角色服务用假实现，返回空列表）。
func newAdminRolesPageEngine(t *testing.T) *gin.Engine {
	t.Helper()
	return newAdminRolesPageEngineWith(t, &fakeRoleService{})
}

// newAdminRolesPageEngineWith 与上面同一装配，但允许注入角色服务（列表装载失败用例需要）。
func newAdminRolesPageEngineWith(t *testing.T, svc *fakeRoleService) *gin.Engine {
	t.Helper()
	handle := adminhttp.NewAdminPagesHandle(nil, svc, nil, nil, nil, nil)
	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		ConfigPath:     support.NewComponentTestConfig(t),
		GinMode:        gin.TestMode,
		InitComponents: true, // 让 shell.Prepare 拿到 i18n / 会话组件
		RouteRegistrar: func(e *gin.Engine) {
			e.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
			e.GET("/admin/roles", handle.RolesPage)
		},
	})
	if err != nil {
		t.Fatalf("隔离依赖已就绪，组件初始化失败: %v", err)
	}
	t.Cleanup(func() { _ = cleanup() })
	return engine
}

// fetchRolesPageErr 带一个构造出来的 ?err= 请求角色页并返回响应体。
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

// rolesPageAlert 取出角色页提示条（role="alert"）里的文本（没有提示条时返回空串）。
//
// 为什么不能对整页做 Contains：顶部语言切换表单的 redirect 隐藏域会回填当前请求 URL，
// 于是 ?err= 的原串也出现在页面里（Jet 已 HTML 转义，但那不是"显示给运营看的提示"）。
// 本用例要断言的正是**显示出来的那一条提示**，所以先定位到提示条再比较。
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

// TestAdminRolesPageErrQueryNoLongerRenders ?err= 不再喂给页面：任何取值都不渲染提示条。
//
// 旧契约下这是「白名单整体匹配」（只有写侧真实产出过的文案才渲染）；本批把结论改成
// 整页提示后，查询参数与页面提示条之间不再有数据通路 —— 手拼 URL 连一条空白提示都造不出来。
func TestAdminRolesPageErrQueryNoLongerRenders(t *testing.T) {
	engine := newAdminRolesPageEngine(t)

	for _, raw := range []string{
		"已删除 3 个角色，2 个未能删除（受保护或被引用）", // 旧契约下会被放行的真实文案
		"一次最多操作 200 项，当前 201 项，请分批进行",
		"已保存\n系统提示：权限已提升\t<ok>", // 换行 + 制表 + 一条像「第二条系统消息」的行
		strings.Repeat("A", 5000),
		"<script>alert(1)</script>",
	} {
		if got := rolesPageAlert(fetchRolesPageErr(t, engine, raw)); got != "" {
			t.Errorf("?err= 不应再渲染任何提示（结论走整页提示），got %q（raw=%q）", got, raw)
		}
	}
}

// 泄漏断言用同包既有的 pageLeakMarkers / assertPageNoLeak（admin_page_err_param_test.go）：
// 那份清单是照「不可信原文」设计的（驱动前缀 + SQLSTATE + 库表名前缀 uq_ / sys_ / pg_），
// 比照 JSON 响应体设计的 leakMarkers 更适合整页断言 —— 后者的 "sort_order" / "cannot unmarshal"
// 恰好也是正常页面的一部分（角色页就有 name="sort_order" 的输入框），对整页断言会误报。

// TestAdminRolesPageFormActionsCarryFilters 列表页写表单的 action 上带筛选上下文。
//
// 同一份键表服务两条路径：渲染时拼进 action 的 query、POST 回来由 shell.BackPath 读回。
// 两处分叉的表现是「写完跳回去筛选静默丢了」——页面不报错、日志也干净，所以必须钉住。
func TestAdminRolesPageFormActionsCarryFilters(t *testing.T) {
	engine := newAdminRolesPageEngine(t)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/roles?keyword=editor&page=2", nil))
	body := recorder.Body.String()

	for _, want := range []string{
		// Jet 会把查询串里的 & 转义成 &amp;（属性值里的实体）。
		`action="/admin/roles/bulk-delete?keyword=editor&amp;page=2"`,
		`action="/admin/roles/create?keyword=editor&amp;page=2"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("写表单 action 应带筛选上下文 %q：%s", want, body)
		}
	}
}

// TestAdminRolesPageListLoadFailureKeepsPage 列表**装载失败**时页面必须还在。
//
// 六个管理页（管理员 / 角色 / 权限点 / 菜单 / 部门 / 数据规则）原先在这一分支
// `c.String(500, pagesMsgAdminGenericFailed)`：浏览器里没有页面，只有一块写着 i18n key 的裸文本 ——
// 侧边栏、筛选框、分页全部消失，运营看到的是 key 而不是一句人话，也无从判断「是我筛错了还是系统坏了」。
// 现在改为降级渲染（空列表 + 归口提示条），本用例把这条契约钉住：
//  1. 状态码仍是 200 且是**完整页面**（筛选框在）；
//  2. 提示条显示归口文案，不是驱动原文，也不是内部标识符；
//  3. 驱动原文（表名 / 约束名 / SQLSTATE）一个片段都不许进响应体。
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

// TestAdminRolesPageLoadFailureIsTheOnlyAlertSource 列表提示条只由装载失败填充。
//
// 装载失败与 URL 里带一条旧 ?err= 可能同时出现：旧 ?err= 不再进页面（结论走整页提示），
// 所以提示条必须**恰好**是装载失败那条，且页面里不出现旧提示的任何文本。
func TestAdminRolesPageLoadFailureIsTheOnlyAlertSource(t *testing.T) {
	engine := newAdminRolesPageEngineWith(t, &fakeRoleService{err: errors.New(dbErrText)})
	// 这条在旧契约下是合法文案（在白名单里），用来证明它不再有任何渲染通路。
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
