package feature

// order_page_load_failure_test.go — 订单 / 优惠码 / 退货三个后台列表页「装载失败」的降级渲染契约。
//
// 背景：三处原先在 `h.projects.List(ctx)` 失败时 `c.String(500, orderenums.ErrInternal)`。
// 浏览器里没有页面，只有一块纯文本 —— 侧栏、页头、筛选器、分页壳全部消失，用户既改不了
// 也退不回去；而那句归口文案还是**硬编码中文常量**（enums.ErrInternal = "操作失败，请稍后重试"），
// 英文界面上照旧显示中文，与页面路径既有的归口文案（shell.PageInternalText）也是两套说法。
// 本批改成降级渲染：空列表 + 归口提示 + HTTP 200 + 页面结构完好（判据与 project 域
// theme_admin_pages.go 的 ThemeManage、admin 六页同一形状）。
//
// 新契约（架构改造后）：页面**不再读取 `?err=`** —— 那条「写动作结论经 URL 回带」的通道
// 连同读侧白名单整批删除，写动作的结论改由 shell.RenderJump 渲染提示页。
// 装载失败的原因由 handler 算好后进模板的 `.LoadErr`，模板据此渲染**一条 `role="alert"`
// 的 badge** + 空态段落「这一页的数据没能读出来」。所以本文件的断言落在 `.LoadErr` 上，
// 不再依赖已删除的 `Err` / `Ok` / `Done` 三个键，也不再有「压过旧 ?err=」这件事。
//
// 本文件钉住三件事，都是「错了会静默」的那种：
//  1. 装载失败仍渲染**完整页面**（响应体含 </html>），页头与筛选框都在；
//  2. 提示条是**受控归口文案**（当前语言的译文），驱动原文一个片段都不进响应体；
//  3. 空态与「还没有数据」分得开 —— 装载失败时不得显示「还没有站点工程」/「这个工程还没有…」，
//     那会把用户引去建工程或改筛选，而真正的原因在页顶那条提示里。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	orderhttp "go_wp/internal/module/order/inbound/http"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/templates"
	"go_wp/public/test/support"
)

// orderPageLoadErrText 模拟基础设施故障的原文：驱动前缀 + 表名 + SQLSTATE。
// 它长得就像会泄漏，一个片段都不许进响应体。
const orderPageLoadErrText = `pq: relation "projects" does not exist (SQLSTATE 42P01)`

// orderPageLeakMarkers 页面响应体里绝不能出现的内部细节片段（照 PostgreSQL 真实报错拼）。
var orderPageLeakMarkers = []string{"SQLSTATE", "42P01", "pq:", `relation "`}

// orderPageLoadFailedTitle 装载失败空态的标题（模板 t() 兜底与 406 的词条同值）。
const orderPageLoadFailedTitle = "这一页的数据没能读出来"

// orderPageInternalText 归口文案（shell.PageInternalText：词条 MsgInternalError 的译文，
// 缺词条时回落同一句中文兜底）。
const orderPageInternalText = "系统内部错误，请稍后重试"

// fakeOrderProjectService 只实现三个页面实际调用到的 List（三处都只用它取工程上下文）。
// 其余方法经嵌入接口转发 —— 一旦被调用就是 panic，正好暴露「页面开始依赖别的能力」。
type fakeOrderProjectService struct {
	projectcontract.ProjectService
	err error
}

// List 注入错误时返回它，否则返回空工程列表（页面走到「还没有站点工程」那一档）。
func (f *fakeOrderProjectService) List(context.Context) ([]projectdto.ProjectResp, error) {
	if f.err != nil {
		return nil, f.err
	}
	return nil, nil
}

// newOrderPageEngine 装配三个列表页（真实 Jet 渲染 + 组件初始化：判据是整页断言）。
func newOrderPageEngine(t *testing.T, projects projectcontract.ProjectService) *gin.Engine {
	t.Helper()
	orders := orderhttp.NewOrderPageHandle(nil, projects, nil)
	coupons := orderhttp.NewCouponPageHandle(nil, projects)
	returns := orderhttp.NewReturnPageHandle(nil, projects, nil, nil)

	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		ConfigPath:     support.NewComponentTestConfig(t),
		GinMode:        gin.TestMode,
		InitComponents: true, // shell.Prepare 需要 i18n / 会话组件
		RouteRegistrar: func(e *gin.Engine) {
			e.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
			e.GET("/admin/orders", orders.OrdersPage)
			e.GET("/admin/coupons", coupons.CouponsPage)
			e.GET("/admin/returns", returns.ReturnsPage)
		},
	})
	if err != nil {
		t.Fatalf("隔离依赖已就绪，组件初始化失败: %v", err)
	}
	t.Cleanup(func() { _ = cleanup() })
	return engine
}

// fetchOrderPage 请求一个列表页并返回响应体（状态码非 200 直接失败）。
func fetchOrderPage(t *testing.T, engine *gin.Engine, path, rawQuery string) string {
	t.Helper()
	target := path
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s 状态码 %d（降级渲染必须仍是 200）: %s", target, recorder.Code, recorder.Body.String())
	}
	return recorder.Body.String()
}

// orderPageAlert 取页面第一条提示条（role="alert"）里的文本。
//
// 与 admin 的同一手法：整页 Contains 分不清「显示出来的提示」与「URL 回填在隐藏域里的原串」，
// 所以先定位到提示条再比较。
func orderPageAlert(body string) string {
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

// assertOrderPageNoLeak 断言整页响应体里不含任何内部细节指纹。
func assertOrderPageNoLeak(t *testing.T, where, body string) {
	t.Helper()
	for _, marker := range orderPageLeakMarkers {
		if strings.Contains(body, marker) {
			t.Errorf("%s 泄漏内部细节 %q", where, marker)
		}
	}
}

// orderPageCases 三个列表页的路径（同一档空态、同一套断言）。
var orderPageCases = []struct{ name, path string }{
	{"订单", "/admin/orders"},
	{"优惠码", "/admin/coupons"},
	{"退货入库", "/admin/returns"},
}

// misdirectingEmptyTexts 装载失败时不得出现的「真的没有数据」空态文案。
//
// 出现它们即意味着：用户被告知「还没有站点工程」/「这个工程还没有订单」，于是去建工程或改筛选，
// 而这次请求的真实原因（工程列表没读出来）只是页顶的一条提示。
var misdirectingEmptyTexts = []string{"还没有站点工程", "这个工程还没有"}

// TestOrderPageLoadFailureDegradesToFullPage 装载失败时三个页面都降级渲染完整页面。
//
// 判据三条一起看才成立：状态码 200（fetchOrderPage 里断言）、页壳与筛选框还在（页面没被换成纯文本）、
// 提示条是受控归口文案（不是驱动原文、也不是裸 key）。
func TestOrderPageLoadFailureDegradesToFullPage(t *testing.T) {
	engine := newOrderPageEngine(t, &fakeOrderProjectService{err: errors.New(orderPageLoadErrText)})

	for _, tc := range orderPageCases {
		t.Run(tc.name, func(t *testing.T) {
			body := fetchOrderPage(t, engine, tc.path, "")

			if !strings.Contains(body, "</html>") {
				t.Fatalf("%s 装载失败未渲染完整页面（缺 </html>）—— 页壳被整块吃掉", tc.path)
			}
			// 页面骨架：页头 + 列表卡 + 表头（原实现是一块纯文本，这三样都不在）。
			//
			// 不断言筛选框：coupons 页的筛选栏由 `{{if selectedProject}}` 包裹 ——
			// 装载失败时工程上下文为空（selected 只可能来自 URL，本页不去读列表），
			// 于是筛选栏本就不渲染。这不是降级渲染的退化，而是「没有工程作用域就没有可筛的东西」
			// 这条既有设计在降级路径上的自然结果。
			for _, marker := range []string{`class="page-head"`, `class="card list-card"`, "<thead>"} {
				if !strings.Contains(body, marker) {
					t.Errorf("%s 装载失败后页面骨架必须还在（缺 %s）", tc.path, marker)
				}
			}

			alert := orderPageAlert(body)
			if !strings.Contains(alert, orderPageInternalText) {
				t.Errorf("%s 提示条应是归口文案 %q，got %q", tc.path, orderPageInternalText, alert)
			}
			assertOrderPageNoLeak(t, tc.path+" 装载失败", body)
		})
	}
}

// TestOrderPageLoadFailureKeepsEmptyStateHonest 装载失败的空态不得冒充「真的没有数据」。
//
// 这一档空态由 handler 算好的 LoadFailed 承载（模板里的判据只有 len(.Projects) == 0，
// 它分不清「没有工程」与「这一次没读出来」）。
func TestOrderPageLoadFailureKeepsEmptyStateHonest(t *testing.T) {
	engine := newOrderPageEngine(t, &fakeOrderProjectService{err: errors.New(orderPageLoadErrText)})

	for _, tc := range orderPageCases {
		t.Run(tc.name, func(t *testing.T) {
			body := fetchOrderPage(t, engine, tc.path, "")

			if !strings.Contains(body, orderPageLoadFailedTitle) {
				t.Errorf("%s 装载失败时空态应显示 %q", tc.path, orderPageLoadFailedTitle)
			}
			for _, misleading := range misdirectingEmptyTexts {
				if strings.Contains(body, misleading) {
					t.Errorf("%s 装载失败时空态出现了误导文案 %q（把「没读出来」说成「没有数据」）",
						tc.path, misleading)
				}
			}
			// 对照组在同一页面上的另一半：空态的「去建工程」引导不能出现（工程本来就存在，
			// 只是这一次读不出来）。
			if strings.Contains(body, `href="/admin/pages"`) {
				t.Errorf("%s 装载失败时空态不应给出「去页面管理建工程」的引导", tc.path)
			}
		})
	}
}

// TestOrderPageLoadFailureIgnoresStaleErrParam 页面不再读取 ?err=：伪造的旧提示既不能上提示条、
// 也不能出现在正文任何位置。
//
// 旧契约下这里守的是「装载失败压过 URL 里那条旧的 ?err=」（两条提示可能同时存在）。
// 新契约把「写动作结论经 URL 回带」这条通道整块删掉了：页面只认 `.LoadErr`（本次请求真实发生
// 的事），`?err=` 连读都不读。于是判据从「装载失败优先」升级为「伪造参数完全不可见」——
// 手拼一个「看起来像业务文案」的串，它一个字都上不了页面。
func TestOrderPageLoadFailureIgnoresStaleErrParam(t *testing.T) {
	engine := newOrderPageEngine(t, &fakeOrderProjectService{err: errors.New(orderPageLoadErrText)})
	// 这条是写侧真实产出过的业务文案（在 orderenums.UserFacingMessages 白名单里）——
	// 旧契约下它会被当成可信回显；新契约下它必须一个字都上不了页面。
	stale := "订单不存在" // order.err.orderNotFound 的 zh-CN 译文，由 180 的 seed 写入
	for _, tc := range orderPageCases {
		t.Run(tc.name, func(t *testing.T) {
			body := fetchOrderPage(t, engine, tc.path, "err="+url.QueryEscape(stale))

			alert := orderPageAlert(body)
			if !strings.Contains(alert, orderPageInternalText) {
				t.Errorf("%s 装载失败应显示归口文案，got %q", tc.path, alert)
			}
			// 伪造的 ?err= 不得出现在正文任何位置（不只是提示条）——「查询参数不是可信边界」
			// 这条判据的形态从「过白名单」升级为「整块删掉读取路径」。
			if strings.Contains(body, stale) {
				t.Errorf("%s 正文里出现了伪造的 ?err= 文案 %q —— 页面不应再读取该参数", tc.path, stale)
			}
		})
	}
}

// TestOrderPageEmptyStateStillShownWhenLoadSucceeds 反证：真的没有工程时，空态仍是「还没有站点工程」。
//
// 没有这一条，「装载失败的空态」可以通过「把所有空态都换掉」蒙过去 —— 那会让正常空态失去
// 它该给的引导（去建一个工程）。
func TestOrderPageEmptyStateStillShownWhenLoadSucceeds(t *testing.T) {
	engine := newOrderPageEngine(t, &fakeOrderProjectService{})

	for _, tc := range orderPageCases {
		t.Run(tc.name, func(t *testing.T) {
			body := fetchOrderPage(t, engine, tc.path, "")

			if !strings.Contains(body, "还没有站点工程") {
				t.Errorf("%s 无工程时应显示「还没有站点工程」空态", tc.path)
			}
			if strings.Contains(body, orderPageLoadFailedTitle) {
				t.Errorf("%s 装载成功时不得显示装载失败空态", tc.path)
			}
			// 装载成功且页面不读 ?err=，因此不应有任何提示条（.LoadErr 为空）。
			if orderPageAlert(body) != "" {
				t.Errorf("%s 装载成功时不应有提示条，got %q", tc.path, orderPageAlert(body))
			}
		})
	}
}
