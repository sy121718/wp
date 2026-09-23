package feature

// admin_page_err_render_test.go — ?err= 进模板前的受控化（页面级断言）。
//
// 与 admin_page_err_param_test.go 的分工：那一条守的是「服务端写进 ?err= 的**文案**是否受控
// （必须是本模块白名单里的业务文案）」，本文件守的是另一半 ——「从 ?err= 读回来的**任意串**
// 能不能渲染」。第四批起判据升级为**白名单整体匹配**：只有写侧真实产出过的文案才渲染，
// 其余一律空串。这正是本文件存在的理由 —— 前者拦不住直接手写 URL 的人：
//   GET /admin/roles?err=任意文本
// 过去一样会把内容渲染在提示条里（Jet 已做 HTML 转义，所以不是 XSS；问题是「看起来像系统说的话」）。
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

// rolesPageAlert 取出角色页提示条（role="alert"）里的文本。
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

// TestAdminRolesPageErrParamIsSanitized ?err= 的四种构造：白名单文案 / 超限提示 / 超长 / 伪造。
//
// 断言的是第四批之后的契约：**只有写侧真实产出过的文案才渲染，其余一律空串**。
// 旧契约（清洗后原样透出）在 2026-09-19 第四批被判定为伪造面 —— 手拼 URL 就能
// 往页面上塞一条顶着「系统提示」样式的消息。
func TestAdminRolesPageErrParamIsSanitized(t *testing.T) {
	engine := newAdminRolesPageEngine(t)

	t.Run("写侧真实产出的批量结论原样渲染", func(t *testing.T) {
		// adminBulkResultURL 在 skipped > 0 时的真实产出（数字归一后整体匹配）。
		want := "已删除 3 个角色，2 个未能删除（受保护或被引用）"
		got := rolesPageAlert(fetchRolesPageErr(t, engine, want))
		if got != want {
			t.Fatalf("写侧真实产出的文案应原样显示，got %q", got)
		}
	})

	t.Run("批量 id 超限提示原样渲染", func(t *testing.T) {
		// shell.BulkIDsFacingText 的产出，读侧候选取的是同一份模板。
		want := "一次最多操作 200 项，当前 201 项，请分批进行"
		got := rolesPageAlert(fetchRolesPageErr(t, engine, want))
		if got != want {
			t.Fatalf("批量超限提示应原样显示，got %q", got)
		}
	})

	t.Run("超长参数不再渲染", func(t *testing.T) {
		// 旧契约截断到 200 字节照常显示；新契约下截断后的串对不上任何候选 → 空串。
		got := rolesPageAlert(fetchRolesPageErr(t, engine, strings.Repeat("A", 5000)))
		if got != "" {
			t.Fatalf("超长参数不应渲染任何内容，got %q", got)
		}
	})

	t.Run("伪造串（含控制字符）不再渲染", func(t *testing.T) {
		// 换行 + 制表 + 一个看起来像「第二条系统消息」的行 —— 过去会被清洗后照常显示。
		got := rolesPageAlert(fetchRolesPageErr(t, engine, "已保存\n系统提示：权限已提升\t<ok>"))
		if got != "" {
			t.Fatalf("伪造串不应渲染任何内容（系统文案必须是系统说过的话），got %q", got)
		}
	})
}

// 泄漏断言用同包既有的 pageLeakMarkers / assertPageNoLeak（admin_page_err_param_test.go）：
// 那份清单是照「不可信原文」设计的（驱动前缀 + SQLSTATE + 库表名前缀 uq_ / sys_ / pg_），
// 比照 JSON 响应体设计的 leakMarkers 更适合整页断言 —— 后者的 "sort_order" / "cannot unmarshal"
// 恰好也是正常页面的一部分（角色页就有 name="sort_order" 的输入框），对整页断言会误报。

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

// TestAdminRolesPageListLoadFailureBeatsStaleErr 装载失败要压过 URL 里那条旧的 ?err=。
//
// 两条提示可能同时存在：上一次写失败回带 ?err=、这一次列表又查不出来。装载失败是**当前这次
// 请求真实发生的事**，必须盖住旧提示，否则页面显示的是一条与本次无关的话。
func TestAdminRolesPageListLoadFailureBeatsStaleErr(t *testing.T) {
	engine := newAdminRolesPageEngineWith(t, &fakeRoleService{err: errors.New(dbErrText)})
	// 这条本身是合法文案（在白名单里），用来证明压过它的不是「白名单拒绝」而是装载失败优先。
	stale := "已删除 3 个角色，2 个未能删除（受保护或被引用）"
	body := fetchRolesPageErr(t, engine, stale)

	const wantAlert = "操作失败，请稍后重试"
	got := rolesPageAlert(body)
	if got != wantAlert {
		t.Fatalf("装载失败应盖过 ?err= 旧提示，got %q", got)
	}
	if strings.Contains(body, stale) && got == stale {
		t.Fatal("旧提示不应占据提示条")
	}
	assertPageNoLeak(t, "角色列表装载失败（带旧 ?err=）", body)
}
