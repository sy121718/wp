package feature

import (
	"context"
	"errors"
	"go_wp/internal/adminui"
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

const orderPageLoadErrText = `pq: relation "projects" does not exist (SQLSTATE 42P01)`

var orderPageLeakMarkers = []string{"SQLSTATE", "42P01", "pq:", `relation "`}

const orderPageLoadFailedTitle = "这一页的数据没能读出来"

const orderPageInternalText = "系统内部错误，请稍后重试"

type fakeOrderProjectService struct {
	projectcontract.ProjectService
	err error
}

func (f *fakeOrderProjectService) List(context.Context) ([]projectdto.ProjectResp, error) {
	if f.err != nil {
		return nil, f.err
	}
	return nil, nil
}

func newOrderPageEngine(t *testing.T, projects projectcontract.ProjectService) *gin.Engine {
	t.Helper()
	orders := orderhttp.NewOrderPageHandle(nil, projects, nil)
	coupons := orderhttp.NewCouponPageHandle(nil, projects)
	returns := orderhttp.NewReturnPageHandle(nil, projects, nil, nil)

	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		ConfigPath:     support.NewComponentTestConfig(t),
		GinMode:        gin.TestMode,
		InitComponents: true,
		RouteRegistrar: func(e *gin.Engine) {
			e.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)

			e.GET("/admin/orders", adminui.PageSlot(orderhttp.OrderListPageTemplate), orders.OrdersPage)
			e.GET("/admin/coupons", adminui.PageSlot(orderhttp.CouponListPageTemplate), coupons.CouponsPage)
			e.GET("/admin/returns", adminui.PageSlot(orderhttp.ReturnListPageTemplate), returns.ReturnsPage)
		},
	})
	if err != nil {
		t.Fatalf("隔离依赖已就绪，组件初始化失败: %v", err)
	}
	t.Cleanup(func() { _ = cleanup() })
	return engine
}

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

func assertOrderPageNoLeak(t *testing.T, where, body string) {
	t.Helper()
	for _, marker := range orderPageLeakMarkers {
		if strings.Contains(body, marker) {
			t.Errorf("%s 泄漏内部细节 %q", where, marker)
		}
	}
}

var orderPageCases = []struct{ name, path string }{
	{"订单", "/admin/orders"},
	{"优惠码", "/admin/coupons"},
	{"退货入库", "/admin/returns"},
}

var misdirectingEmptyTexts = []string{"还没有站点工程", "这个工程还没有"}

func TestOrderPageLoadFailureDegradesToFullPage(t *testing.T) {
	engine := newOrderPageEngine(t, &fakeOrderProjectService{err: errors.New(orderPageLoadErrText)})

	for _, tc := range orderPageCases {
		t.Run(tc.name, func(t *testing.T) {
			body := fetchOrderPage(t, engine, tc.path, "")

			if !strings.Contains(body, "</html>") {
				t.Fatalf("%s 装载失败未渲染完整页面（缺 </html>）—— 页壳被整块吃掉", tc.path)
			}

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

			if strings.Contains(body, `href="/admin/pages"`) {
				t.Errorf("%s 装载失败时空态不应给出「去页面管理建工程」的引导", tc.path)
			}
		})
	}
}

func TestOrderPageLoadFailureIgnoresStaleErrParam(t *testing.T) {
	engine := newOrderPageEngine(t, &fakeOrderProjectService{err: errors.New(orderPageLoadErrText)})

	stale := "订单不存在"
	for _, tc := range orderPageCases {
		t.Run(tc.name, func(t *testing.T) {
			body := fetchOrderPage(t, engine, tc.path, "err="+url.QueryEscape(stale))

			alert := orderPageAlert(body)
			if !strings.Contains(alert, orderPageInternalText) {
				t.Errorf("%s 装载失败应显示归口文案，got %q", tc.path, alert)
			}

			if strings.Contains(body, stale) {
				t.Errorf("%s 正文里出现了伪造的 ?err= 文案 %q —— 页面不应再读取该参数", tc.path, stale)
			}
		})
	}
}

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

			if orderPageAlert(body) != "" {
				t.Errorf("%s 装载成功时不应有提示条，got %q", tc.path, orderPageAlert(body))
			}
		})
	}
}
