package runtimefragment

// orders_test.go — 访客订单片段的端点级测试。
//
// 这里验的是**片段这一层**的三件事：
//   · 身份如实传下去（未登录不报 401，而是渲染一句引导 —— 401 会让 HTMX 静默不替换节点）；
//   · 参数与身份进到 order 契约的请求里（片段自己不做归属判断，但必须把 userID 交出去）；
//   · 没有工程 id 时给出可见结论而不是空壳。
//
// 归属本身由 order 模块的 SQL 条件负责，用例见 public/test/order/feature/order_visitor_test.go。

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
	usercontract "go_wp/internal/module/user/contract"
	"go_wp/internal/templates"
)

// fakeVisitorOrders 记录收到的请求并返回固定结果。
type fakeVisitorOrders struct {
	listReq   *orderdto.VisitorOrderListReq
	detailReq *orderdto.VisitorOrderDetailReq
	list      *orderdto.VisitorOrderListResp
	detail    *orderdto.OrderDetailResp
	err       error
}

func (f *fakeVisitorOrders) ListVisitorOrders(_ context.Context, req *orderdto.VisitorOrderListReq) (*orderdto.VisitorOrderListResp, error) {
	f.listReq = req
	return f.list, f.err
}

func (f *fakeVisitorOrders) GetVisitorOrder(_ context.Context, req *orderdto.VisitorOrderDetailReq) (*orderdto.OrderDetailResp, error) {
	f.detailReq = req
	return f.detail, f.err
}

// TestOrderFragmentTemplatesRender 两个模板都要能渲染。
//
// 这一条单独存在的理由：模板错了的表现是「片段渲染失败」五个字（端点把 error
// 收成了一句话），线上看不出是哪个字段写错了。这里直接把模板渲染一次的原始错误暴露出来。
func TestOrderFragmentTemplatesRender(t *testing.T) {
	if _, err := templates.RenderFragment("order_list", ordersFragmentData{FragmentType: "ordersList", Tabs: orderStatusTabs(&Request{}, "p", "", "")}); err != nil {
		t.Fatalf("order_list 模板渲染失败: %v", err)
	}
	if _, err := templates.RenderFragment("order_detail", orderDetailFragmentData{FragmentType: "orderDetail"}); err != nil {
		t.Fatalf("order_detail 模板渲染失败: %v", err)
	}
}

// callFragment 直接调片段端点（不经路由），返回响应体。
func callFragment(t *testing.T, typeName, query string, visitorID uint64) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/_fragments/"+typeName+"?"+query, nil)
	c.Params = gin.Params{{Key: "type", Value: typeName}}
	if visitorID != 0 {
		c.Set(usercontract.VisitorContextKey, visitorID)
	}
	FragmentEndpoint(c)
	return w.Body.String()
}

// TestOrdersFragmentWithoutLoginShowsGuide 未登录：给引导，不报 401。
func TestOrdersFragmentWithoutLoginShowsGuide(t *testing.T) {
	fake := &fakeVisitorOrders{}
	SetVisitorOrderReader(fake)
	t.Cleanup(func() { SetVisitorOrderReader(nil) })

	body := callFragment(t, "ordersList", "projectId=proj-1", 0)
	if !strings.Contains(body, "登录后可以查看你的订单") {
		t.Fatalf("未登录应渲染引导文案；实际：%s", body)
	}
	if fake.listReq != nil {
		t.Fatal("未登录时不该向订单域发起查询")
	}
}

// TestOrdersFragmentPassesVisitorIdentity 已登录：身份必须进到订单域的请求里。
func TestOrdersFragmentPassesVisitorIdentity(t *testing.T) {
	fake := &fakeVisitorOrders{list: &orderdto.VisitorOrderListResp{
		Total: 1,
		List: []*orderdto.OrderResp{{
			ID: 42, OrderNo: "GWP2026010100000001", Status: "paid",
			Total: 12345, CreateTime: time.Date(2026, 1, 1, 10, 30, 0, 0, time.UTC),
		}},
	}}
	SetVisitorOrderReader(fake)
	t.Cleanup(func() { SetVisitorOrderReader(nil) })

	body := callFragment(t, "ordersList", "projectId=proj-1&limit=20&status=paid", 77)
	if fake.listReq == nil {
		t.Fatal("已登录时应向订单域查询")
	}
	// 这三条是这一层的全部责任：把身份与参数**如实**交出去。
	if fake.listReq.UserID != 77 {
		t.Fatalf("访客身份未传给订单域，实际 %d", fake.listReq.UserID)
	}
	if fake.listReq.ProjectID != "proj-1" || fake.listReq.Status != "paid" || fake.listReq.Limit != 20 {
		t.Fatalf("参数未如实传递: %+v", fake.listReq)
	}
	for _, want := range []string{"GWP2026010100000001", "已付款", "123.45 元"} {
		if !strings.Contains(body, want) {
			t.Errorf("片段缺少 %q；实际：%s", want, body)
		}
	}
}

// TestOrderDetailFragmentKeepsOwnershipArgs 详情片段：订单 id 与身份一起传下去。
func TestOrderDetailFragmentKeepsOwnershipArgs(t *testing.T) {
	fake := &fakeVisitorOrders{detail: &orderdto.OrderDetailResp{
		Head: &orderdto.OrderResp{ID: 9, OrderNo: "NO-9", Status: "shipped", Total: 100, CreateTime: time.Now()},
		Items: []*orderdto.OrderItemResp{{
			ProductName: "杯子", VariantLabel: "白色", SKU: "SKU-1",
			UnitPrice: 100, Quantity: 1, LineTotal: 100,
		}},
	}}
	SetVisitorOrderReader(fake)
	t.Cleanup(func() { SetVisitorOrderReader(nil) })

	body := callFragment(t, "orderDetail", "projectId=proj-1&orderId=9", 5)
	if fake.detailReq == nil || fake.detailReq.OrderID != 9 || fake.detailReq.UserID != 5 {
		t.Fatalf("详情请求缺少订单 id 或身份: %+v", fake.detailReq)
	}
	for _, want := range []string{"NO-9", "已发货", "SKU-1"} {
		if !strings.Contains(body, want) {
			t.Errorf("详情片段缺少 %q；实际：%s", want, body)
		}
	}
}

// TestOrdersFragmentWithoutReaderShowsUnavailable 未接入时给可见提示，而不是空壳。
func TestOrdersFragmentWithoutReaderShowsUnavailable(t *testing.T) {
	SetVisitorOrderReader(nil)
	body := callFragment(t, "ordersList", "projectId=proj-1", 7)
	if !strings.Contains(body, msgOrdersUnavailable) {
		t.Fatalf("未接入应渲染提示；实际：%s", body)
	}
}

// TestOrdersFragmentRejectsNonNumericOrderID 非数字的订单 id 直接给结论，不打到下游。
func TestOrdersFragmentRejectsNonNumericOrderID(t *testing.T) {
	fake := &fakeVisitorOrders{}
	SetVisitorOrderReader(fake)
	t.Cleanup(func() { SetVisitorOrderReader(nil) })
	body := callFragment(t, "orderDetail", "projectId=proj-1&orderId=abc", 5)
	if fake.detailReq != nil {
		t.Fatal("非法 id 不该传到订单域（会让 PostgreSQL 的 uuid/bigint 解析报错）")
	}
	if strings.TrimSpace(body) == "" {
		t.Fatal("非法 id 也要给出可见结论")
	}
}
