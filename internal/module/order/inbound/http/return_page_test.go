package orderhttp

import (
	pageview "go_wp/pkg/view"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	ginrender "github.com/gin-gonic/gin/render"

	"go_wp/internal/adminui"
	ordercontract "go_wp/internal/module/order/contract"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
)

type returnLabelsService struct {
	ordercontract.OrderService
	list      *orderdto.ReturnListResp
	detail    *orderdto.ReturnDetailResp
	listErr   error
	detailErr error
}

func (s *returnLabelsService) ListReturns(_ context.Context, _ *orderdto.ReturnListReq) (*orderdto.ReturnListResp, error) {

	if s.list == nil && s.listErr == nil {
		return &orderdto.ReturnListResp{}, nil
	}
	return s.list, s.listErr
}

func (s *returnLabelsService) GetReturn(_ context.Context, _ uint64) (*orderdto.ReturnDetailResp, error) {
	return s.detail, s.detailErr
}

type returnPageCapture struct {
	delegate ginrender.HTMLRender
	name     string
	data     gin.H
	err      error
}

func (r *returnPageCapture) Instance(name string, data any) ginrender.Render {
	r.name, r.data = name, data.(gin.H)
	return r
}

func (r *returnPageCapture) Render(w http.ResponseWriter) error {
	r.err = r.delegate.Instance(r.name, r.data).Render(w)
	return r.err
}

func (r *returnPageCapture) WriteContentType(w http.ResponseWriter) {
	r.delegate.Instance(r.name, r.data).WriteContentType(w)
}

func adminTemplatesDir(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller 拿不到本文件路径")
	}
	dir := filepath.Join(filepath.Dir(self), "..", "..", "..", "..", "templates")
	if _, err := os.Stat(filepath.Join(dir, "admin", "order", "returns.html")); err != nil {
		t.Fatalf("模板目录定位失败: %v", err)
	}
	return dir
}

func renderReturnsPage(t *testing.T, query string, svc *returnLabelsService) string {
	t.Helper()
	rec := httptest.NewRecorder()
	c, engine := gin.CreateTestContext(rec)
	cap := &returnPageCapture{delegate: templates.NewJetHTMLRender(adminTemplatesDir(t), true)}
	engine.HTMLRender = cap
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/returns"+query, nil)
	pageview.Slot(ReturnListPageTemplate, adminui.RenderPage)(c)
	NewReturnPageHandle(svc, orderPaginationProjects{}, nil, nil).ReturnsPage(c)
	c.Writer.WriteHeaderNow()
	if cap.err != nil {
		t.Fatalf("模板渲染失败: %v", cap.err)
	}
	if cap.name != "admin/order/returns.html" {
		t.Fatalf("模板名 = %q", cap.name)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	if strings.Contains(body, "{{") {
		t.Fatalf("输出残留模板语法字面量（渲染中途截断）")
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "</html>") {
		t.Fatalf("页面没有渲染完整（末尾不是 </html>），末尾 = %q", tailOf(body, 120))
	}
	return body
}

func tailOf(s string, n int) string {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) <= n {
		return trimmed
	}
	return trimmed[len(trimmed)-n:]
}

func returnStatusSampleDetail(returnStatus, orderStatus string) *orderdto.ReturnDetailResp {
	return &orderdto.ReturnDetailResp{
		Return: &orderdto.ReturnResp{
			ID: 7, ReturnNo: "R-7", OrderID: 3, OrderNo: "O-3",
			Status: returnStatus, StatusTone: "info",
		},
		Order: &orderdto.OrderResp{
			ID: 3, OrderNo: "O-3", Status: orderStatus, StatusTone: "ok",
			Currency: "CNY", Total: 12345,
		},
	}
}

func TestReturnStatusFactsMirrorEnumsTruth(t *testing.T) {
	cases := []struct {
		name   string
		status string
	}{
		{"已知：待审核", "requested"},
		{"已知：待收货", "approved"},
		{"已知：已入库待退款", "received"},
		{"已知：已完成", "completed"},
		{"已知：已拒绝", "rejected"},
		{"已知：已撤销", "cancelled"},
		{"空串显示破折号", ""},
		{"纯空白等同空串", " \t\n "},
		{"带空白的已知值仍命中词条", "  received  "},
		{"未知值原样保留（含首尾空白，不 trim）", " unknown "},
		{"未知值里的 HTML 原样交给模板转义", "<b>x</b>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			facts := returnStatusPageFacts([]string{tc.status}, "", nil, nil)
			got, ok := facts[tc.status]
			if !ok {
				t.Fatalf("map 键必须是**原始**状态值 %q，实际键表 = %v", tc.status, keysOf(facts))
			}
			key, fallback := orderenums.ReturnStatusLabel(tc.status)
			if got.Key != key || got.Fallback != fallback {
				t.Fatalf("事实 (%q, %q) 与枚举真源 (%q, %q) 不一致", got.Key, got.Fallback, key, fallback)
			}
		})
	}
}

func TestOrderStatusFactsMirrorEnumsTruth(t *testing.T) {
	for _, status := range []string{"pending", "paid", "shipped", "completed", "cancelled", "refunded", "", " \t ", "  shipped  ", " nope "} {
		t.Run(status, func(t *testing.T) {
			detail := &orderdto.ReturnDetailResp{Order: &orderdto.OrderResp{Status: status}}
			got, ok := orderStatusPageFacts(nil, detail)[status]
			if !ok {
				t.Fatalf("订单状态 %q 没进事实表", status)
			}
			key, fallback := orderenums.OrderStatusLabel(status)
			if got.Key != key || got.Fallback != fallback {
				t.Fatalf("事实 (%q, %q) 与枚举真源 (%q, %q) 不一致", got.Key, got.Fallback, key, fallback)
			}
		})
	}
}

func TestStatusPageFactsCollectEveryRenderedStatus(t *testing.T) {
	returns := []*orderdto.ReturnResp{
		nil,
		{Status: "approved"},
		{Status: "approved"},
		{Status: "rejected"},
	}
	detail := returnStatusSampleDetail("requested", "paid")

	returnFacts := returnStatusPageFacts(returnStatusValues, "rejected", returns, detail)
	for _, want := range append(append([]string{}, returnStatusValues...), "approved", "rejected", "requested") {
		if _, ok := returnFacts[want]; !ok {
			t.Fatalf("退货事实表缺少 %q，键表 = %v", want, keysOf(returnFacts))
		}
	}

	if got := returnFacts[""]; got.Key != "" || got.Fallback != "—" {
		t.Fatalf("空状态事实 = %+v", got)
	}

	orderFacts := orderStatusPageFacts(orderStatusValues, detail)
	for _, want := range append(append([]string{}, orderStatusValues...), "paid") {
		if _, ok := orderFacts[want]; !ok {
			t.Fatalf("订单事实表缺少 %q，键表 = %v", want, keysOf(orderFacts))
		}
	}

	for _, tc := range []struct {
		name   string
		detail *orderdto.ReturnDetailResp
	}{
		{"无详情", nil},
		{"详情里没有退货单", &orderdto.ReturnDetailResp{Order: &orderdto.OrderResp{Status: "paid"}}},
		{"详情里没有订单", &orderdto.ReturnDetailResp{Return: &orderdto.ReturnResp{Status: "approved"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := returnStatusPageFacts(returnStatusValues, "", nil, tc.detail)
			if len(facts) != len(returnStatusValues) {
				t.Fatalf("只应含白名单取值（无筛选、无列表、无详情），实际 %v", keysOf(facts))
			}
			_ = orderStatusPageFacts(orderStatusValues, tc.detail)
		})
	}
}

func TestStatusLabelFactHasNoFunctionField(t *testing.T) {
	typ := reflect.TypeOf(StatusLabelFact{})
	if typ.NumField() != 2 {
		t.Fatalf("事实字段数 = %d，want 2", typ.NumField())
	}
	for i := 0; i < typ.NumField(); i++ {
		if kind := typ.Field(i).Type.Kind(); kind == reflect.Func {
			t.Fatalf("字段 %s 是函数类型", typ.Field(i).Name)
		}
	}
}

func TestReturnsPageRendersStatusLabelsFromFacts(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		orderenums.ReturnStatusKeyRequested:      {"en-US": "Awaiting review", "zh-CN": "待审核"},
		orderenums.OrderStatusKeyPrefix + "paid": {"en-US": "Paid", "zh-CN": "已付款"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	svc := func() *returnLabelsService {
		return &returnLabelsService{
			list: &orderdto.ReturnListResp{
				Total: 1,
				List:  []*orderdto.ReturnResp{{ID: 9, ReturnNo: "R-9", Status: "approved", StatusTone: "mute"}},
			},
			detail: returnStatusSampleDetail("requested", "paid"),
		}
	}

	for _, tc := range []struct {
		lang          string
		wantRequested string
		wantOrderPaid string
	}{
		{"zh-CN", "待审核", "已付款"},
		{"en-US", "Awaiting review", "Paid"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			body := renderReturnsPage(t, "?project=p-1&returnId=7&lang="+tc.lang, svc())

			if !strings.Contains(body, `badge-info">`+tc.wantRequested+`</span>`) {
				t.Fatalf("详情退货状态未按 %s 取词", tc.lang)
			}

			if !strings.Contains(body, `badge-ok">`+tc.wantOrderPaid+`</span>`) {
				t.Fatalf("摘要订单状态未按 %s 取词", tc.lang)
			}

			if !strings.Contains(body, `badge-mute">待收货</span>`) {
				t.Fatalf("列表行状态未取自退货事实表（缺词条应回落兜底文本）")
			}
		})
	}
}

func TestReturnsPageStatusLabelsHandleUnknownAndEmpty(t *testing.T) {
	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	t.Run("未知状态原样且被转义", func(t *testing.T) {
		body := renderReturnsPage(t, "?project=p-1&returnId=7", &returnLabelsService{
			detail: returnStatusSampleDetail("<script>alert(1)</script>", "paid"),
		})
		if !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
			t.Fatalf("未知状态没有按原值转义输出")
		}
		if strings.Contains(body, "<script>alert(1)</script>") {
			t.Fatalf("未知状态被当成 HTML 直接输出（转义失效）")
		}
	})

	t.Run("纯空白状态显示破折号", func(t *testing.T) {
		body := renderReturnsPage(t, "?project=p-1", &returnLabelsService{
			list: &orderdto.ReturnListResp{
				Total: 1,
				List:  []*orderdto.ReturnResp{{ID: 5, ReturnNo: "R-5", Status: "   ", StatusTone: "mute"}},
			},
		})
		if !strings.Contains(body, `badge-mute">—</span>`) {
			t.Fatalf("空白状态应显示破折号")
		}
	})

	t.Run("空列表下的未知筛选值原样显示", func(t *testing.T) {
		body := renderReturnsPage(t, "?project=p-1&status=unknown-status", &returnLabelsService{
			list: &orderdto.ReturnListResp{Total: 0},
		})
		if !strings.Contains(body, "unknown-status") {
			t.Fatalf("空态筛选提示没有显示原始筛选值")
		}
	})
}

func keysOf(facts map[string]StatusLabelFact) []string {
	out := make([]string, 0, len(facts))
	for k := range facts {
		out = append(out, k)
	}
	return out
}
