// order_shipment_page_test.go — S3（迁移 627 / 625）的验收：待发货页 / 发货表单 / 承运商页。
//
// 覆盖任务书 §8.6 的会红断言：
//
//	1  待发货页只出现 status='paid' 且剩余 > 0 的订单
//	   （构造 paid 全发完 / shipped / pending 三张单，只出现第一张）
//	2  表单「本次」输入框 max = 剩余数量（HTML 片段断言）
//	3  承运商模板带 {order_no} → 400 + 兜底文案，库中该行未写入
//	4  模板文件不出现金额减法（template_logic_test.go 已由 internal/architecture 守）
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

// newShipPagesEngine 装配只挂发货三屏的测试引擎（真实 Jet 模板 + 真实 service）。
//
// 不挂鉴权中间件：登录态与权限属装配层（另有 check-page-endpoint-authz.sh 守），
// 本用例的对象是**页面形状**（哪些行出现、max 是多少）。
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

// TestPendingShipPageOnlyListsShippableOrders 验收 1：
// 只出现 paid 且还有剩余的单（paid 全发完 / shipped / pending 都不出现）。
func TestPendingShipPageOnlyListsShippableOrders(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	_, vid := f.addProduct(t, "待发货列表商品", 50, 20)
	engine := newShipPagesEngine(t, f)

	// ① 应当出现的：paid 且有剩余。
	paidOrder, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 2))
	if err != nil {
		t.Fatalf("建单失败: %v", err)
	}
	f.payOrder(t, paidOrder.ID)

	// ② 不该出现的：pending（没付款）。
	pendingOrder, err := f.orders.CreateOrder(ctx, f.createBaseReq(vid, 1))
	if err != nil {
		t.Fatalf("建 pending 单失败: %v", err)
	}

	// ③ 不该出现的：paid 但已全部发完。
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

// TestShipFormMaxIsRemaining 验收 2：表单「本次」输入框的 max = 剩余数量。
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
	// 先发 2 件 —— 表单的 max 应当是 3（剩余），不是 5（订单数量）。
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
	// max 必须是剩余 3 —— 这条断言钉的正是「模板不做 Quantity − Shipped」：
	// 若模板自己算，max 会跟着页面算出的值走，与 service 的口径分叉就看不出来了。
	if !strings.Contains(body, `max="3"`) {
		t.Fatalf("「本次」输入框的 max 应为剩余 3（不是订单数量 5），实际 HTML：%s", clipHTML(body))
	}
	if strings.Contains(body, `max="5"`) {
		t.Fatalf("max 不该是订单数量 5（那会让超量发货在前端就通过）")
	}
}

// TestCarrierTemplateRejectsForeignPlaceholders 验收 3：
// 模板带 {order_no} → 业务错误，且库里没有写入这一行。
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

	// 合法模板可以建（{tracking_no} 是唯一允许的占位符）。
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
	// 大小写不敏感的唯一性：再建一个 sf 应打回。
	if _, derr := f.orders.CreateCarrier(ctx, &orderdto.CarrierCreateReq{
		ProjectID: f.projectID, Code: "sf", Name: "顺丰二号",
	}); derr == nil || !strings.Contains(derr.Error(), orderenums.ErrCarrierCodeTaken) {
		t.Fatalf("同工程同代码（大小写不同）应报 ErrCarrierCodeTaken，实际 %v", derr)
	}
}

// TestCarrierPageRendersList 承运商页能渲染（含停用的 —— 否则「停用后不见了」= 以为被删了）。
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
	// 启用的与停用的都要在（配置页要能看到并重新启用停用的那些）。
	if !strings.Contains(body, active.Name) {
		t.Fatalf("承运商页应列出启用的 %q", active.Name)
	}
	if !strings.Contains(body, disabled.Name) {
		t.Fatalf("承运商页应列出停用的 %q（否则「停用后不见了」会让人以为被删了）", disabled.Name)
	}
}

// clipHTML 截断长 HTML（失败信息里贴整页没有意义）。
func clipHTML(s string) string {
	if len(s) <= 1200 {
		return s
	}
	return s[:1200] + "…"
}
