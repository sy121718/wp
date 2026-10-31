package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	orderhttp "go_wp/internal/module/order/inbound/http"
	"go_wp/internal/templates"
)

func newShipPagesEngine(t *testing.T, f *orderFixture) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	handle := orderhttp.NewOrderShipmentPageHandle(f.orders, f.projects)
	engine.GET("/admin/orders/pending-ship",
		adminui.PageSlot(orderhttp.PendingShipPageTemplate), handle.PendingShipPage)
	engine.GET("/admin/orders/ship-form",
		adminui.BareSlot(orderhttp.ShipFormPageTemplate), handle.ShipFormFragment)
	engine.GET("/admin/order-carriers",
		adminui.PageSlot(orderhttp.OrderCarriersPageTemplate), handle.OrderCarriersPage)
	return engine
}

func TestPendingShipPageOnlyListsShippableOrders(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "待发货列表商品", 50, 20)
	engine := newShipPagesEngine(t, f)

	paidOrder, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 2))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	f.payOrder(t, paidOrder.ID)

	pendingOrder, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 1))
	if err != nil {
		t.Fatalf("建 pending 单失败: %v", err)
	}

	shippedOrder, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 1))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	f.payOrder(t, shippedOrder.ID)
	itemIDs := f.orderItemIDs(t, shippedOrder.ID)
	if _, serr := f.orders.ShipOrder(ctx, &orderdto.ShipOrderReq{
		OrderID: shippedOrder.ID, ProjectID: f.projectID, RequestID: "page-full-ship",
		Lines: []orderdto.ShipLineReq{{OrderItemID: itemIDs[0], Quantity: 1}},
	}); serr != nil {
		t.Fatalf("全量发货失败: %v", serr)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/orders/pending-ship?project="+f.projectID, nil)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("待发货页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, paidOrder.OrderNo) {
		t.Fatalf("待发货页应列出 paid 且有剩余的订单 %s", paidOrder.OrderNo)
	}
	if strings.Contains(body, pendingOrder.OrderNo) {
		t.Fatalf("待发货页不该列出未付款的订单 %s", pendingOrder.OrderNo)
	}
	if strings.Contains(body, shippedOrder.OrderNo) {
		t.Fatalf("待发货页不该列出已全部发完的订单 %s", shippedOrder.OrderNo)
	}
}

func TestShipFormMaxIsRemaining(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "发货表单商品", 50, 20)
	created, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 5))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	f.payOrder(t, created.ID)

	itemIDs := f.orderItemIDs(t, created.ID)
	if _, serr := f.orders.ShipOrder(ctx, &orderdto.ShipOrderReq{
		OrderID: created.ID, ProjectID: f.projectID, RequestID: "form-partial",
		Lines: []orderdto.ShipLineReq{{OrderItemID: itemIDs[0], Quantity: 2}},
	}); serr != nil {
		t.Fatalf("部分发货失败: %v", serr)
	}

	engine := newShipPagesEngine(t, f)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/admin/orders/ship-form?orderId="+strconv.FormatUint(created.ID, 10)+"&projectId="+f.projectID, nil)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("发货表单应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	if !strings.Contains(body, `max="3"`) {
		t.Fatalf("「本次」输入框的 max 应为剩余 3（不是订单数量 5），实际 HTML：%s", clipHTML(body))
	}
	if strings.Contains(body, `max="5"`) {
		t.Fatalf("max 不该是订单数量 5（那会让超量发货在前端就通过）")
	}
}

func TestCarrierTemplateRejectsForeignPlaceholders(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, err := f.orders.CreateCarrier(ctx, &orderdto.CarrierCreateReq{
		ProjectID: f.projectID, Code: "BAD", Name: "坏模板",
		TrackingURLTemplate: "https://example.com/track?order={order_no}",
	})
	if err == nil || !strings.Contains(err.Error(), orderenums.ErrCarrierTemplateInvalid) {
		t.Fatalf("非 {tracking_no} 占位符应报 ErrCarrierTemplateInvalid，实际 %v", err)
	}
	var n int64
	if qerr := f.db.Raw("SELECT COUNT(*) FROM order_carriers WHERE project_id = ? AND upper(code) = 'BAD'",
		f.projectID).Scan(&n).Error; qerr != nil {
		t.Fatalf("查承运商失败: %v", qerr)
	}
	if n != 0 {
		t.Fatalf("模板非法时不该写入承运商，实际 %d 行", n)
	}

	ok, cerr := f.orders.CreateCarrier(ctx, &orderdto.CarrierCreateReq{
		ProjectID: f.projectID, Code: "SF", Name: "顺丰",
		TrackingURLTemplate: "https://example.com/track?no={tracking_no}",
	})
	if cerr != nil {
		t.Fatalf("合法模板应能建承运商: %v", cerr)
	}
	if ok.Code != "SF" || ok.Status != "active" {
		t.Fatalf("建出的承运商字段不对: %+v", ok)
	}

	if _, derr := f.orders.CreateCarrier(ctx, &orderdto.CarrierCreateReq{
		ProjectID: f.projectID, Code: "sf", Name: "顺丰二号",
	}); derr == nil || !strings.Contains(derr.Error(), orderenums.ErrCarrierCodeTaken) {
		t.Fatalf("同工程同代码（大小写不同）应报 ErrCarrierCodeTaken，实际 %v", derr)
	}
}

func TestCarrierPageRendersList(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	active, err := f.orders.CreateCarrier(ctx, &orderdto.CarrierCreateReq{
		ProjectID: f.projectID, Code: "SF", Name: "顺丰速运",
	})
	if err != nil {
		t.Fatalf("建承运商失败: %v", err)
	}
	disabled, err := f.orders.CreateCarrier(ctx, &orderdto.CarrierCreateReq{
		ProjectID: f.projectID, Code: "YTO", Name: "圆通",
	})
	if err != nil {
		t.Fatalf("建承运商失败: %v", err)
	}
	off := "disabled"
	if _, uerr := f.orders.UpdateCarrier(ctx, &orderdto.CarrierUpdateReq{
		ID: disabled.ID, ProjectID: f.projectID, Status: &off,
	}); uerr != nil {
		t.Fatalf("停用承运商失败: %v", uerr)
	}

	engine := newShipPagesEngine(t, f)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/order-carriers?project="+f.projectID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("承运商页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	if !strings.Contains(body, active.Name) {
		t.Fatalf("承运商页应列出启用的 %q", active.Name)
	}
	if !strings.Contains(body, disabled.Name) {
		t.Fatalf("承运商页应列出停用的 %q（否则「停用后不见了」会让人以为被删了）", disabled.Name)
	}
}

func clipHTML(s string) string {
	if len(s) <= 1200 {
		return s
	}
	return s[:1200] + "…"
}

func TestShipmentPagesCarryTheirOwnTitle(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	engine := newShipPagesEngine(t, f)

	cases := []struct {
		name string
		path string
		want string
	}{

		{"待发货页", "/admin/orders/pending-ship?project=" + f.projectID, "<title>待发货"},
		{"承运商页", "/admin/order-carriers?project=" + f.projectID, "<title>承运商"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("应 200，实际 %d：%s", rec.Code, clipHTML(rec.Body.String()))
			}
			body := rec.Body.String()
			if !strings.Contains(body, tc.want) {
				t.Errorf("页签标题没有取自视图（缺 %q）：视图字段大概率漏了 view:\"title\" tag，"+
					"layout 读 .title 取不到就回落成品牌名。实际片段：%s",
					tc.want, clipHTML(body[:min(len(body), 400)]))
			}
			if strings.Contains(body, "<title>管理后台") {
				t.Errorf("标题退化成品牌名了（视图的 Title 没有摊平到 .title 键）")
			}
		})
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
